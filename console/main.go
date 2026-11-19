// Command console serves the GeoGrid support desk console: a web page to see the
// support tickets, file new ones as a customer, and ask the support-triage agent
// to triage them while each step of its work shows up as it happens.
//
// It reads and files tickets in Gitea as the support-inbox bot, calls
// support-triage over A2A, and reads the customer's account in Lago.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"support-desk/internal/agentkit"
)

//go:embed web
var webFiles embed.FS

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("console stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	repo := agentkit.Env("SUPPORT_REPO", "geogrid/support")
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("SUPPORT_REPO must look like owner/repo, got %q", repo)
	}

	gitea := newGiteaClient(
		agentkit.Env("GITEA_URL", "http://localhost:3001"),
		// For the "Open in Gitea" links, when the console reaches Gitea at an
		// address the browser cannot.
		agentkit.Env("GITEA_PUBLIC_URL", ""),
		owner, name,
		// support-inbox's token: the console lists tickets and files new ones as
		// the inbox.
		agentkit.Env("GITEA_TOKEN_FILE", ".secrets/support-inbox.token"),
	)

	var lago *lagoClient
	if key := agentkit.Env("LAGO_API_KEY", ""); key != "" {
		lago = newLagoClient(agentkit.Env("LAGO_API_URL", "http://localhost:3000/api/v1"), key)
	} else {
		slog.Warn("LAGO_API_KEY is not set, so the account panel stays empty")
	}

	triage := newTriager(agentkit.Env("TRIAGE_AGENT_URL", "http://localhost:8001"))

	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}
	s := &server{
		gitea:      gitea,
		lago:       lago,
		triage:     triage,
		lagoUIURL:  strings.TrimRight(agentkit.Env("LAGO_PUBLIC_URL", ""), "/"),
		web:        web,
		repoLabel:  repo,
		agentCards: newCardCache(),
	}

	srv := &http.Server{
		Addr:              agentkit.Env("LISTEN_ADDR", ":8080"),
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("console starting", "listen", srv.Addr, "repo", repo, "triage_agent", triage.baseURL,
		"lago", lago != nil)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
