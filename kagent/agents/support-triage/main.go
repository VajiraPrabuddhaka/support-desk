// Command support-triage triages GeoGrid support tickets, as a kagent runtime.
// It is the same agent as /agents/support-triage, written for kagent: kagent's
// controller starts it in a Substrate sandbox and hands it what the platform
// declared for it.
//
// What comes from kagent, and so from the Agent on OpenChoreo:
//   - the system prompt and the skills
//   - the model (a ModelConfig), with its key kept outside the sandbox
//   - Gitea's MCP server and the tools the model may see
//
// What stays in this program: the rule that keeps triage inside the support
// repository, and the hand-off of billing tickets to billing-agent over A2A.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"

	"support-desk/kagent/internal/kit"
)

// allowedWriteMethods are the issue_write methods triage may use. issue_write
// can also create, edit and close issues and delete comments; guardTools
// refuses those before they reach Gitea.
var allowedWriteMethods = map[string]bool{"add_labels": true, "add_comment": true}

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("support-triage stopped", "error", err)
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

	repo := kit.Env("SUPPORT_REPO", "geogrid/support")
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("SUPPORT_REPO must look like owner/repo, got %q", repo)
	}

	// billing-agent is another agent on kagent, reached through kagent's
	// gateway. Wrapped as a tool, it answers and triage stays in charge of the
	// ticket.
	billingURL, err := kit.RequireEnv("BILLING_AGENT_URL")
	if err != nil {
		return err
	}
	billing, err := kit.RemoteAgent("billing_agent",
		"GeoGrid billing specialist. Send one self-contained request with the customer's "+
			"account ID, the requester's email and what they ask about: invoices, balances, duplicate "+
			"charges, refunds or credits, coupons. It returns findings, actions taken, what needs a "+
			"person, and a draft reply to the customer.",
		billingURL)
	if err != nil {
		return fmt.Errorf("create billing_agent client: %w", err)
	}

	base, err := rt.Instruction()
	if err != nil {
		return err
	}
	instruction := strings.NewReplacer("{{REPO}}", repo, "{{OWNER}}", owner, "{{NAME}}", name).Replace(base)

	logBefore, logAfter := kit.ToolLogging()
	triage, err := llmagent.New(llmagent.Config{
		Name: "support_triage",
		Description: "Triages GeoGrid support tickets in Gitea: sets type and priority labels, " +
			"asks billing_agent about billing tickets, and writes a triage note with a suggested reply.",
		Model: llm,
		// InstructionProvider passes the prompt as is. Instruction would
		// treat {...} as session-state placeholders.
		InstructionProvider: func(agent.ReadonlyContext) (string, error) { return instruction, nil },
		Tools:               []tool.Tool{billing},
		// Gitea's MCP server holds the triage bot's token and acts as the bot in
		// Gitea, so the agent sends no credentials.
		Toolsets:            rt.Toolsets(ctx),
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{logBefore, guardTools(owner, name)},
		AfterToolCallbacks:  []llmagent.AfterToolCallback{logAfter},
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	slog.Info("support-triage starting", "model", rt.ModelName(), "repo", repo,
		"mcp_servers", rt.MCPServers(), "billing_agent", billingURL)
	return rt.Serve(triage)
}

// guardTools refuses Gitea calls outside the support repository and
// issue_write methods other than adding labels and comments. A refused call
// never reaches Gitea; the model gets the error back as the tool result.
func guardTools(owner, repo string) llmagent.BeforeToolCallback {
	return func(_ agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		switch t.Name() {
		case "list_issues", "issue_read", "issue_write", "label_read":
		default:
			return nil, nil
		}
		if o, _ := args["owner"].(string); o != "" && !strings.EqualFold(o, owner) {
			return refuse(t.Name(), "only the "+owner+"/"+repo+" repository is allowed"), nil
		}
		if r, _ := args["repo"].(string); r != "" && !strings.EqualFold(r, repo) {
			return refuse(t.Name(), "only the "+owner+"/"+repo+" repository is allowed"), nil
		}
		if t.Name() == "issue_write" {
			method, _ := args["method"].(string)
			if !allowedWriteMethods[method] {
				return refuse(t.Name(), "method "+method+" is not allowed; use add_labels or add_comment"), nil
			}
		}
		return nil, nil
	}
}

func refuse(toolName, reason string) map[string]any {
	slog.Warn("tool call refused", "tool", toolName, "reason", reason)
	return map[string]any{"error": "refused by support-triage policy: " + reason}
}
