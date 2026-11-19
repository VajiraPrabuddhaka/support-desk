// Command billing-agent answers billing questions about GeoGrid customers.
// It reads and changes billing data in Lago through Lago's MCP server, and
// other agents call it over A2A.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"

	"support-desk/internal/agentkit"
)

// builtinPrompt and builtinSkills are used when the platform mounts no prompt or
// skills (AGENT_SYSTEM_PROMPT_FILE, AGENT_SKILLS_DIR), as in Docker Compose. On
// OpenChoreo the Agent's system prompt and workspace skills take their place.
//
//go:embed prompt.md
var builtinPrompt string

//go:embed skills
var builtinSkills embed.FS

// lagoTools are the Lago MCP tools the model may see. Everything else the
// server offers (creating customers, invoices, plans, credit notes, deleting
// coupons, ...) is left out. Of these, only void_invoice and apply_coupon
// change data, and the prompt limits when.
var lagoTools = []string{
	"get_customer", "list_customers", "get_customer_current_usage",
	"list_invoices", "list_customer_invoices", "get_invoice", "find_invoice_by_number", "download_invoice",
	"get_fee",
	"list_payments", "list_customer_payments", "get_payment",
	"list_credit_notes", "get_credit_note",
	"list_coupons", "get_coupon", "list_applied_coupons",
	"list_subscriptions", "list_customer_subscriptions", "get_subscription",
	"list_plans", "get_plan",
	"void_invoice", "apply_coupon",
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("billing-agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	llm, modelName, err := agentkit.NewModel(ctx, "BILLING_MODEL")
	if err != nil {
		return err
	}

	// Lago's MCP server holds the Lago API key, so the agent sends no
	// credentials.
	lagoMCP := agentkit.Env("LAGO_MCP_URL", "http://localhost:8092/mcp")
	lago, err := agentkit.MCPToolset(lagoMCP, lagoTools)
	if err != nil {
		return fmt.Errorf("connect to Lago MCP: %w", err)
	}

	skills, err := fs.Sub(builtinSkills, "skills")
	if err != nil {
		return err
	}
	instruction, err := agentkit.LoadInstruction(builtinPrompt, skills)
	if err != nil {
		return err
	}
	genConfig, err := agentkit.GenerateConfig()
	if err != nil {
		return err
	}
	logBefore, logAfter := agentkit.ToolLogging()
	billing, err := llmagent.New(llmagent.Config{
		Name: "billing_agent",
		Description: "GeoGrid billing specialist. Send one self-contained request with the customer's " +
			"account ID, the requester's email and what they ask about: invoices, balances, duplicate " +
			"charges, refunds or credits, coupons. It verifies the requester, looks the account up in " +
			"Lago, takes only the actions its policy allows, and returns findings, actions taken, " +
			"what needs a person, and a draft reply to the customer.",
		Model:                 llm,
		GenerateContentConfig: genConfig,
		InstructionProvider: func(agent.ReadonlyContext) (string, error) {
			return instruction + "\nToday's date is " + time.Now().UTC().Format("2006-01-02") + " (UTC).\n", nil
		},
		Toolsets:            []tool.Toolset{lago},
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{logBefore},
		AfterToolCallbacks:  []llmagent.AfterToolCallback{logAfter},
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	slog.Info("billing-agent starting", "model", modelName, "lago_mcp", lagoMCP)
	return agentkit.Serve(ctx, agentkit.ServeConfig{
		Agent:      billing,
		ListenAddr: agentkit.Env("LISTEN_ADDR", ":8002"),
		PublicURL:  agentkit.Env("PUBLIC_URL", "http://localhost:8002"),
		Version:    "1.0.0",
	})
}
