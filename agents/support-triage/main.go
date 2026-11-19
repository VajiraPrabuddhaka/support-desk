// Command support-triage triages GeoGrid support tickets. Tickets are issues
// in a Gitea repository, which it reads and labels through Gitea's MCP
// server. For billing tickets it asks billing-agent over A2A.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent/remoteagent/v2"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"

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

// giteaTools are the Gitea MCP tools the model may see.
var giteaTools = []string{"list_issues", "issue_read", "issue_write", "search_issues", "label_read"}

// allowedWriteMethods are the issue_write methods triage may use. issue_write
// can also create, edit and close issues and delete comments; guardTools
// refuses those before they reach Gitea.
var allowedWriteMethods = map[string]bool{"add_labels": true, "add_comment": true}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("support-triage stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	llm, modelName, err := agentkit.NewModel(ctx, "TRIAGE_MODEL")
	if err != nil {
		return err
	}

	repo := agentkit.Env("SUPPORT_REPO", "geogrid/support")
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("SUPPORT_REPO must look like owner/repo, got %q", repo)
	}

	// Gitea's MCP server holds the triage bot's token and acts as the bot in
	// Gitea, so the agent sends no credentials.
	giteaMCP := agentkit.Env("GITEA_MCP_URL", "http://localhost:8091/mcp")
	gitea, err := agentkit.MCPToolset(giteaMCP, giteaTools)
	if err != nil {
		return fmt.Errorf("connect to Gitea MCP: %w", err)
	}

	// billing-agent runs as its own service. Its agent card says where to
	// send A2A calls; wrapped as a tool, it answers and triage stays in charge
	// of the ticket.
	billingURL := agentkit.Env("BILLING_AGENT_URL", "http://localhost:8002")
	billing, err := remoteagent.NewA2A(remoteagent.A2AConfig{
		Name: "billing_agent",
		Description: "GeoGrid billing specialist. Send one self-contained request with the customer's " +
			"account ID, the requester's email and what they ask about: invoices, balances, duplicate " +
			"charges, refunds or credits, coupons. It returns findings, actions taken, what needs a " +
			"person, and a draft reply to the customer.",
		AgentCardProvider: agentkit.AgentCardProvider(billingURL, agentkit.Env("A2A_ALLOW_HTTP", "") == "true"),
	})
	if err != nil {
		return fmt.Errorf("create billing_agent client: %w", err)
	}

	skills, err := fs.Sub(builtinSkills, "skills")
	if err != nil {
		return err
	}
	base, err := agentkit.LoadInstruction(builtinPrompt, skills)
	if err != nil {
		return err
	}
	instruction := strings.NewReplacer("{{REPO}}", repo, "{{OWNER}}", owner, "{{NAME}}", name).Replace(base)
	genConfig, err := agentkit.GenerateConfig()
	if err != nil {
		return err
	}
	logBefore, logAfter := agentkit.ToolLogging()
	triage, err := llmagent.New(llmagent.Config{
		Name: "support_triage",
		Description: "Triages GeoGrid support tickets in Gitea: sets type and priority labels, " +
			"asks billing_agent about billing tickets, and writes a triage note with a suggested reply.",
		Model:                 llm,
		GenerateContentConfig: genConfig,
		// InstructionProvider passes the prompt as is. Instruction would
		// treat {...} as session-state placeholders.
		InstructionProvider: func(agent.ReadonlyContext) (string, error) { return instruction, nil },
		Tools:               []tool.Tool{agenttool.New(billing, nil)},
		Toolsets:            []tool.Toolset{gitea},
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{logBefore, guardTools(owner, name)},
		AfterToolCallbacks:  []llmagent.AfterToolCallback{logAfter},
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	slog.Info("support-triage starting", "model", modelName, "repo", repo,
		"gitea_mcp", giteaMCP, "billing_agent", billingURL)
	return agentkit.Serve(ctx, agentkit.ServeConfig{
		Agent:      triage,
		ListenAddr: agentkit.Env("LISTEN_ADDR", ":8001"),
		PublicURL:  agentkit.Env("PUBLIC_URL", "http://localhost:8001"),
		Version:    "1.0.0",
		// The console shows each step of a triage as it happens.
		StreamSteps: true,
	})
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
