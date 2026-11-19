package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// giteaClient reads and files tickets in the support repository.
type giteaClient struct {
	baseURL   string
	publicURL string // where a browser reaches Gitea; empty if it is baseURL
	owner     string
	repo      string
	tokenFile string
	http      *http.Client
}

func newGiteaClient(baseURL, publicURL, owner, repo, tokenFile string) *giteaClient {
	return &giteaClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		publicURL: strings.TrimRight(publicURL, "/"),
		owner:     owner,
		repo:      repo,
		tokenFile: tokenFile,
		http:      &http.Client{Timeout: 20 * time.Second},
	}
}

// link returns a Gitea link that a browser can open. Gitea builds the links in
// its replies from the address it was called on, so the links the console gets
// hold Gitea's internal address (http://gitea:3000 in Docker Compose, the
// Service on OpenChoreo), which a browser cannot reach.
func (g *giteaClient) link(u string) string {
	if g.publicURL == "" {
		return u
	}
	if rest, ok := strings.CutPrefix(u, g.baseURL); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
		return g.publicURL + rest
	}
	return u
}

type giteaLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type giteaUser struct {
	Login    string `json:"login"`
	FullName string `json:"full_name"`
}

type giteaIssue struct {
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	State     string       `json:"state"`
	HTMLURL   string       `json:"html_url"`
	Comments  int          `json:"comments"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Labels    []giteaLabel `json:"labels"`
	User      giteaUser    `json:"user"`
}

type giteaComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	User      giteaUser `json:"user"`
}

// listIssues returns the repository's issues, newest first.
func (g *giteaClient) listIssues(ctx context.Context) ([]giteaIssue, error) {
	var issues []giteaIssue
	if err := g.do(ctx, http.MethodGet, g.repoPath("/issues?state=all&type=issues&limit=50"), nil, &issues); err != nil {
		return nil, err
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number > issues[j].Number })
	return issues, nil
}

func (g *giteaClient) getIssue(ctx context.Context, number int) (giteaIssue, error) {
	var issue giteaIssue
	err := g.do(ctx, http.MethodGet, g.repoPath(fmt.Sprintf("/issues/%d", number)), nil, &issue)
	return issue, err
}

func (g *giteaClient) listComments(ctx context.Context, number int) ([]giteaComment, error) {
	var comments []giteaComment
	err := g.do(ctx, http.MethodGet, g.repoPath(fmt.Sprintf("/issues/%d/comments", number)), nil, &comments)
	return comments, err
}

func (g *giteaClient) createIssue(ctx context.Context, title, body string) (giteaIssue, error) {
	var issue giteaIssue
	err := g.do(ctx, http.MethodPost, g.repoPath("/issues"), map[string]string{"title": title, "body": body}, &issue)
	return issue, err
}

func (g *giteaClient) repoPath(suffix string) string {
	return "/api/v1/repos/" + g.owner + "/" + g.repo + suffix
}

// do sends one request with the token read from the token file, so a rotated
// token is picked up without a restart.
func (g *giteaClient) do(ctx context.Context, method, path string, body, out any) error {
	raw, err := os.ReadFile(g.tokenFile)
	if err != nil {
		return fmt.Errorf("read Gitea token: %w", err)
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+strings.TrimSpace(string(raw)))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitea: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &upstreamError{Service: "Gitea", Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// upstreamError is an error answer from Gitea or Lago.
type upstreamError struct {
	Service string
	Status  int
	Body    string
}

func (e *upstreamError) Error() string {
	body := e.Body
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	return fmt.Sprintf("%s answered HTTP %d: %s", e.Service, e.Status, body)
}
