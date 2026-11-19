// Command seed fills Gitea and Lago with the GeoGrid support scenarios:
//
//   - Gitea: the geogrid organization, the private support repository, the
//     support-inbox and triage-bot accounts, the triage labels, and five
//     customer tickets. It writes each bot's API token to
//     $SECRETS_DIR/<bot>.token: triage-bot's for the support-triage agent, and
//     support-inbox's for the console.
//   - Lago: the Growth plan, two add-ons, the LAUNCH20 coupon, and five
//     customers with subscriptions and invoices.
//
// It talks only to the public APIs of both systems. Running it again creates
// nothing twice.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"support-desk/internal/agentkit"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
	slog.Info("seed complete")
}

func run(ctx context.Context) error {
	adminPassword, err := agentkit.RequireEnv("GITEA_ADMIN_PASSWORD")
	if err != nil {
		return err
	}
	lagoKey, err := agentkit.RequireEnv("LAGO_API_KEY")
	if err != nil {
		return err
	}

	gitea, err := newGiteaSeeder(
		agentkit.Env("GITEA_URL", "http://localhost:3001"),
		agentkit.Env("GITEA_ADMIN_USER", "gitea-admin"),
		adminPassword,
		agentkit.Env("SUPPORT_REPO", "geogrid/support"),
		agentkit.Env("SECRETS_DIR", ".secrets"),
	)
	if err != nil {
		return err
	}
	lago := newLagoSeeder(agentkit.Env("LAGO_API_URL", "http://localhost:3000"), lagoKey)

	if err := lago.run(ctx); err != nil {
		return fmt.Errorf("lago: %w", err)
	}
	if err := gitea.run(ctx); err != nil {
		return fmt.Errorf("gitea: %w", err)
	}
	return nil
}
