// Command billing-agent answers billing questions about GeoGrid customers, as a
// kagent runtime. It is the same agent as /agents/billing-agent, written for
// kagent: kagent's controller starts it in a Substrate sandbox and hands it what
// the platform declared for it.
//
// What comes from kagent, and so from the Agent on OpenChoreo:
//   - the system prompt and the skills
//   - the model (a ModelConfig), with its key kept outside the sandbox
//   - Lago's MCP server and the tools the model may see
//
// The list of Lago tools is the Agent's (spec.tools.mcp[].tools), and kagent
// shows the model nothing else. The server also offers tools that create and
// delete billing data, so an Agent without that list would expose them all.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"

	"support-desk/kagent/internal/kit"
)

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("billing-agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	rt, ctx, err := kit.Load(ctx)
	if err != nil {
		return err
	}
	llm, err := rt.Model(ctx)
	if err != nil {
		return err
	}
	instruction, err := rt.Instruction()
	if err != nil {
		return err
	}

	logBefore, logAfter := kit.ToolLogging()
	billing, err := llmagent.New(llmagent.Config{
		Name: "billing_agent",
		Description: "GeoGrid billing specialist. Send one self-contained request with the customer's " +
			"account ID, the requester's email and what they ask about: invoices, balances, duplicate " +
			"charges, refunds or credits, coupons. It verifies the requester, looks the account up in " +
			"Lago, takes only the actions its policy allows, and returns findings, actions taken, " +
			"what needs a person, and a draft reply to the customer.",
		Model: llm,
		// The date is read on every request: a sandbox starts from a snapshot of
		// this process, so a date read at startup would be the snapshot's.
		InstructionProvider: func(agent.ReadonlyContext) (string, error) {
			return instruction + "\nToday's date is " + time.Now().UTC().Format("2006-01-02") + " (UTC).\n", nil
		},
		// Lago's MCP server holds the Lago API key, so the agent sends no
		// credentials.
		Toolsets:            rt.Toolsets(ctx),
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{logBefore},
		AfterToolCallbacks:  []llmagent.AfterToolCallback{logAfter},
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	slog.Info("billing-agent starting", "model", rt.ModelName(), "mcp_servers", rt.MCPServers())
	return rt.Serve(billing)
}
