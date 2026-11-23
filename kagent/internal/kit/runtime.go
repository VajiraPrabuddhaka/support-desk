// Package kit holds what both agents share on kagent: loading what kagent
// compiled for the agent (its system prompt, model, MCP tools and skills), and
// serving the agent the way kagent's bring-your-own Harness expects.
//
// The agents run as kagent runtimes only. kagent's controller starts them in a
// Substrate sandbox, and its gateway is their only caller. Nothing here works
// outside kagent: for a plain container, see the agents in /agents.
package kit

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/tool"

	kagenta2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	kagentagent "github.com/kagent-dev/kagent/go/adk/pkg/agent"
	kagentapp "github.com/kagent-dev/kagent/go/adk/pkg/app"
	kagentconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	kagentmcp "github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	kagentsession "github.com/kagent-dev/kagent/go/adk/pkg/session"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// The folders that kagent's runtime library writes to. The image creates them
// for the user it runs as (see the Dockerfiles).
const (
	configDir  = "/config"
	pluginsDir = "/plugins"
	dataDir    = "/data/plugins"
)

// Runtime is what kagent compiled for this agent from its Agent, Harness,
// ModelConfig and RemoteMCPServer objects.
type Runtime struct {
	config *configv1.ADKConfig
	card   *a2a.AgentCard
	logger *slog.Logger
}

// Load fetches the bootstrap bundle of this agent's revision from kagent's
// controller and copies the Agent's skills to /skills. The returned context
// carries kagent's logger, which its library reads.
//
// kagent names the bundle in KAGENT_BOOTSTRAP_JSON, so Load fails in a plain
// container.
func Load(ctx context.Context) (*Runtime, context.Context, error) {
	if err := kagentconfig.MaterializeFromBootstrap(ctx, configDir); err != nil {
		return nil, ctx, fmt.Errorf("load the bootstrap bundle (this program runs on kagent only): %w", err)
	}
	// LoadAgentConfigs (plural) is for kagent's own runtime. The two calls below
	// are what kagent's BYO example uses.
	cfg, err := kagentconfig.LoadAgentConfig(filepath.Join(configDir, "config.json"))
	if err != nil {
		return nil, ctx, err
	}
	if cfg.GetAgent() == nil {
		return nil, ctx, fmt.Errorf("kagent compiled no agent configuration")
	}
	card, err := kagentconfig.LoadAgentCard(filepath.Join(configDir, "agent-card.json"))
	if err != nil {
		return nil, ctx, err
	}

	logger, err := logging.New(os.Stderr, cmp.Or(kagentconfig.ProcessConfig(cfg).GetLogLevel(), "info"))
	if err != nil {
		return nil, ctx, err
	}
	slog.SetDefault(logger)
	ctx = logging.IntoContext(ctx, logger)

	if err := kagentconfig.MaterializeAgentPlugins(ctx, cfg.GetAgent(), kagentconfig.AgentPluginPaths{
		Packages: pluginsDir, Skills: kagentconfig.DefaultSkillsRoot, Data: dataDir,
	}); err != nil {
		return nil, ctx, fmt.Errorf("load the skills: %w", err)
	}
	return &Runtime{config: cfg, card: card, logger: logger}, ctx, nil
}

// Name and Namespace are those of the kagent Agent.
func (r *Runtime) Name() string      { return kagentconfig.ProcessConfig(r.config).GetName() }
func (r *Runtime) Namespace() string { return kagentconfig.ProcessConfig(r.config).GetNamespace() }

// Description is the description of the kagent Agent.
func (r *Runtime) Description() string { return r.config.GetAgent().GetDescription() }

// Model returns the model of the Agent's ModelConfig. The API key is not in the
// sandbox: kagent sets a placeholder, and Substrate's egress gateway replaces
// the Authorization header on the way out.
func (r *Runtime) Model(ctx context.Context) (model.LLM, error) {
	m := r.config.GetAgent().GetModel()
	if m == nil {
		return nil, fmt.Errorf("the Agent has no model: declare one under spec.models")
	}
	llm, err := kagentagent.CreateLLM(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("create model %s: %w", m.GetModel(), err)
	}
	return llm, nil
}

// ModelName is the name of the model, for the log.
func (r *Runtime) ModelName() string { return r.config.GetAgent().GetModel().GetModel() }

// Toolsets returns one toolset per MCP server of the Agent. Each shows the model
// only the tools that the Agent lists for that server, so the allowlist is the
// platform's and not this program's.
func (r *Runtime) Toolsets(ctx context.Context) []tool.Toolset {
	a := r.config.GetAgent()
	return kagentmcp.CreateToolsets(ctx, a.GetHttpTools(), a.GetSseTools(), a.GetStdioTools(), false, nil)
}

// MCPServers lists the URLs of the Agent's MCP servers, for the log.
func (r *Runtime) MCPServers() []string {
	var urls []string
	for _, t := range r.config.GetAgent().GetHttpTools() {
		urls = append(urls, t.GetParams().GetUrl())
	}
	return urls
}

// Instruction returns the agent's instruction: the system prompt of the Agent
// followed by its skills. Every skill is added to the instruction, so its rules
// apply to every request.
func (r *Runtime) Instruction() (string, error) {
	prompt := strings.TrimSpace(r.config.GetAgent().GetInstruction())
	if prompt == "" {
		return "", fmt.Errorf("the Agent has no system prompt")
	}
	var skills []skill
	dir := r.config.GetAgent().GetSkillsDirectory()
	if dir != "" {
		var err error
		if skills, err = readSkills(os.DirFS(dir)); err != nil {
			return "", fmt.Errorf("read skills (%s): %w", dir, err)
		}
	}
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.name)
	}
	r.logger.Info("instruction loaded", "prompt_chars", len(prompt), "skills_from", dir, "skills", names)
	return withSkills(prompt, skills), nil
}

// Serve runs the agent until the process is stopped: A2A over gRPC and JSON-RPC
// on the runtime port (80), /readyz on 8081, and every task saved through
// kagent's task store. kagent's executor streams each step of a run to the
// caller and labels tool calls with kagent's own metadata keys.
func (r *Runtime) Serve(root agent.Agent) error {
	rt := kagentconfig.ProcessConfig(r.config)
	if rt.GetControlPlaneUrl() == "" {
		return fmt.Errorf("kagent's config has no control plane URL")
	}
	appName := appName(rt)

	// The session store is in the sandbox's durable folder, as for kagent's own
	// runtime. One sandbox holds one conversation.
	sessions, err := kagentsession.NewService(r.config.GetAgent().GetSessionDbUrl())
	if err != nil {
		return fmt.Errorf("open the session store: %w", err)
	}
	executor, err := kagenta2a.NewKAgentExecutor(kagenta2a.KAgentExecutorConfig{
		RunnerConfig:   runner.Config{AppName: appName, Agent: root, SessionService: sessions},
		SessionService: sessions,
		// Each step of a run still reaches the caller as it happens. This only
		// keeps the model's reply from arriving token by token, which the console
		// would show as a step per fragment.
		Stream:    false,
		AppName:   appName,
		Logger:    r.logger,
		AgentName: cmp.Or(rt.GetName(), root.Name()),
	})
	if err != nil {
		return fmt.Errorf("create the executor: %w", err)
	}

	card := *r.card
	card.Capabilities.Streaming = true
	app, err := kagentapp.New(kagentapp.AppConfig{
		ControlPlaneURL:  rt.GetControlPlaneUrl(),
		MaxContentLength: rt.MaxContentLength,
		AgentCard:        card,
		Host:             rt.GetHost(),
		Port:             fmt.Sprint(cmp.Or(rt.GetPort(), 80)),
		AppName:          appName,
		Logger:           r.logger,
		Agent:            root,
	}, executor)
	if err != nil {
		return fmt.Errorf("create the kagent app: %w", err)
	}
	r.logger.Info("serving A2A on kagent", "agent", root.Name(), "kagent_agent", r.Namespace()+"/"+r.Name())
	return app.Run()
}

// appName is the identity that kagent's own runtime derives for an agent.
func appName(rt *configv1.ADKRuntimeConfig) string {
	if rt.GetNamespace() == "" || rt.GetName() == "" {
		return cmp.Or(rt.GetName(), "agent")
	}
	return strings.ReplaceAll(rt.GetNamespace(), "-", "_") + "__NS__" + strings.ReplaceAll(rt.GetName(), "-", "_")
}
