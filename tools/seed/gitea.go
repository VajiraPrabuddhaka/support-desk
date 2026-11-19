package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	inboxUser  = "support-inbox"
	triageUser = "triage-bot"
)

// botTokens are the API tokens the seed writes to <secretsDir>/<user>.token:
// triage-bot's for the support-triage agent, and support-inbox's for the
// console, which lists tickets and files new ones as the inbox. Both may read
// and write issues (labels and comments are part of issues) and read the
// repository.
var botTokens = []struct{ user, name string }{
	{triageUser, "support-triage-agent"},
	{inboxUser, "support-desk-console"},
}

var tokenScopes = []string{"write:issue", "read:repository", "read:user"}

type giteaSeeder struct {
	url        string
	admin      *apiClient
	owner      string
	repo       string
	secretsDir string
}

func newGiteaSeeder(giteaURL, adminUser, adminPassword, repo, secretsDir string) (*giteaSeeder, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("SUPPORT_REPO must look like owner/repo, got %q", repo)
	}
	return &giteaSeeder{
		url:        giteaURL,
		admin:      newAPIClient(giteaURL, basicAuth(adminUser, adminPassword)),
		owner:      owner,
		repo:       name,
		secretsDir: secretsDir,
	}, nil
}

func basicAuth(user, password string) func(*http.Request) {
	return func(r *http.Request) { r.SetBasicAuth(user, password) }
}

func tokenAuth(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "token "+token) }
}

func (g *giteaSeeder) run(ctx context.Context) error {
	if err := waitFor(ctx, "Gitea and its admin account", 3*time.Minute, func() (bool, error) {
		err := g.admin.do(ctx, http.MethodGet, "/api/v1/user", nil, nil)
		return err == nil, err
	}); err != nil {
		return err
	}

	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"organization", g.ensureOrg},
		{"repository", g.ensureRepo},
		{"bot users", g.ensureUsers},
		{"labels", g.ensureLabels},
		{"bot tokens", g.ensureTokens},
		{"tickets", g.ensureTickets},
	}
	for _, s := range steps {
		if err := s.fn(ctx); err != nil {
			return fmt.Errorf("gitea %s: %w", s.name, err)
		}
	}
	return nil
}

func (g *giteaSeeder) ensureOrg(ctx context.Context) error {
	err := g.admin.do(ctx, http.MethodGet, "/api/v1/orgs/"+g.owner, nil, nil)
	if err == nil || !isStatus(err, http.StatusNotFound) {
		return err
	}
	slog.Info("gitea: creating organization", "org", g.owner)
	return g.admin.do(ctx, http.MethodPost, "/api/v1/orgs", map[string]any{
		"username":   g.owner,
		"full_name":  "GeoGrid",
		"visibility": "private",
	}, nil)
}

func (g *giteaSeeder) ensureRepo(ctx context.Context) error {
	err := g.admin.do(ctx, http.MethodGet, g.repoPath(""), nil, nil)
	if err == nil || !isStatus(err, http.StatusNotFound) {
		return err
	}
	slog.Info("gitea: creating repository", "repo", g.owner+"/"+g.repo)
	return g.admin.do(ctx, http.MethodPost, "/api/v1/orgs/"+g.owner+"/repos", map[string]any{
		"name":        g.repo,
		"private":     true,
		"description": "Customer support tickets. Opened by support-inbox, triaged by triage-bot.",
	}, nil)
}

// ensureUsers creates the two bot accounts and gives them write access to the
// support repository: support-inbox opens tickets and triage-bot labels and
// comments on them.
func (g *giteaSeeder) ensureUsers(ctx context.Context) error {
	for _, user := range []struct{ name, fullName string }{
		{inboxUser, "Support inbox"},
		{triageUser, "Support triage agent"},
	} {
		err := g.admin.do(ctx, http.MethodGet, "/api/v1/users/"+user.name, nil, nil)
		if isStatus(err, http.StatusNotFound) {
			slog.Info("gitea: creating user", "user", user.name)
			err = g.admin.do(ctx, http.MethodPost, "/api/v1/admin/users", map[string]any{
				"username":             user.name,
				"full_name":            user.fullName,
				"email":                user.name + "@geogrid.example.com",
				"password":             randomSecret(),
				"must_change_password": false,
			}, nil)
		}
		if err != nil {
			return err
		}
		if err := g.admin.do(ctx, http.MethodPut, g.repoPath("/collaborators/"+user.name),
			map[string]any{"permission": "write"}, nil); err != nil {
			return fmt.Errorf("add %s as collaborator: %w", user.name, err)
		}
	}
	return nil
}

func (g *giteaSeeder) ensureLabels(ctx context.Context) error {
	var existing []struct {
		Name string `json:"name"`
	}
	if err := g.admin.do(ctx, http.MethodGet, g.repoPath("/labels?limit=50"), nil, &existing); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, l := range existing {
		have[l.Name] = true
	}
	for _, l := range labels {
		if have[l.Name] {
			continue
		}
		if err := g.admin.do(ctx, http.MethodPost, g.repoPath("/labels"), map[string]any{
			"name": l.Name, "color": l.Color, "description": l.Description,
		}, nil); err != nil {
			return fmt.Errorf("create label %s: %w", l.Name, err)
		}
	}
	return nil
}

func (g *giteaSeeder) ensureTokens(ctx context.Context) error {
	for _, t := range botTokens {
		if err := g.ensureToken(ctx, t.user, t.name); err != nil {
			return fmt.Errorf("%s: %w", t.user, err)
		}
	}
	return nil
}

// ensureToken writes a working token for user to <secretsDir>/<user>.token. A
// token already in the file is kept if Gitea still accepts it as that user.
func (g *giteaSeeder) ensureToken(ctx context.Context, user, tokenName string) error {
	path := filepath.Join(g.secretsDir, user+".token")
	if raw, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(raw))
		var me struct {
			Login string `json:"login"`
		}
		if token != "" && newAPIClient(g.url, tokenAuth(token)).do(ctx, http.MethodGet, "/api/v1/user", nil, &me) == nil &&
			me.Login == user {
			slog.Info("gitea: token is still valid", "user", user, "file", path)
			return nil
		}
	}

	// Gitea creates tokens only for a user who signs in with a password, so
	// give the bot a fresh random one. It is not stored anywhere.
	bot, err := g.botClient(ctx, user)
	if err != nil {
		return err
	}
	tokenPath := "/api/v1/users/" + user + "/tokens"
	if err := bot.do(ctx, http.MethodDelete, tokenPath+"/"+url.PathEscape(tokenName), nil, nil); err != nil &&
		!isStatus(err, http.StatusNotFound) {
		return fmt.Errorf("delete old token: %w", err)
	}
	var created struct {
		SHA1 string `json:"sha1"`
	}
	if err := bot.do(ctx, http.MethodPost, tokenPath, map[string]any{
		"name": tokenName, "scopes": tokenScopes,
	}, &created); err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	if created.SHA1 == "" {
		return fmt.Errorf("gitea returned an empty token")
	}
	if err := os.MkdirAll(g.secretsDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(created.SHA1+"\n"), 0o600); err != nil {
		return err
	}
	slog.Info("gitea: wrote token", "user", user, "file", path, "scopes", strings.Join(tokenScopes, ","))
	return nil
}

// ensureTickets opens each scenario ticket as support-inbox, unless an issue
// with the same title exists. It uses the token that ensureTokens wrote, the
// same one the console files tickets with.
func (g *giteaSeeder) ensureTickets(ctx context.Context) error {
	var existing []struct {
		Title string `json:"title"`
	}
	if err := g.admin.do(ctx, http.MethodGet, g.repoPath("/issues?state=all&type=issues&limit=50"), nil, &existing); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, i := range existing {
		have[i.Title] = true
	}

	raw, err := os.ReadFile(filepath.Join(g.secretsDir, inboxUser+".token"))
	if err != nil {
		return fmt.Errorf("read %s's token: %w", inboxUser, err)
	}
	inbox := newAPIClient(g.url, tokenAuth(strings.TrimSpace(string(raw))))
	for _, t := range scenarioTickets {
		if have[t.Title] {
			continue
		}
		var issue struct {
			Number int `json:"number"`
		}
		if err := inbox.do(ctx, http.MethodPost, g.repoPath("/issues"), map[string]any{
			"title": t.Title, "body": t.Body(),
		}, &issue); err != nil {
			return fmt.Errorf("open ticket %q: %w", t.Title, err)
		}
		slog.Info("gitea: opened ticket", "number", issue.Number, "title", t.Title)
	}
	return nil
}

// botClient sets a new random password for a bot account and returns a
// client that signs in as the bot with it.
func (g *giteaSeeder) botClient(ctx context.Context, user string) (*apiClient, error) {
	password := randomSecret()
	if err := g.admin.do(ctx, http.MethodPatch, "/api/v1/admin/users/"+user, map[string]any{
		"login_name": user, "source_id": 0, "password": password, "must_change_password": false,
	}, nil); err != nil {
		return nil, fmt.Errorf("set password for %s: %w", user, err)
	}
	return newAPIClient(g.url, basicAuth(user, password)), nil
}

func (g *giteaSeeder) repoPath(suffix string) string {
	return "/api/v1/repos/" + g.owner + "/" + g.repo + suffix
}

func randomSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
