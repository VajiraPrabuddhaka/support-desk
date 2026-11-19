package agentkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/session"
)

// InvokePath is where an agent accepts A2A JSON-RPC calls. The agent card is
// served at a2asrv.WellKnownAgentCardPath.
const InvokePath = "/a2a"

// ServeConfig describes one agent served over A2A.
type ServeConfig struct {
	Agent agent.Agent
	// ListenAddr is the local address to listen on, such as ":8002".
	ListenAddr string
	// PublicURL is the base URL that other agents use to reach this one. It
	// goes into the agent card, so inside Docker it is the service name
	// (http://billing-agent:8002), not localhost.
	PublicURL string
	// Version is the agent version shown in the agent card.
	Version string
	// StreamSteps sends every step of a run to the caller as it happens (one
	// A2A artifact per ADK event): the model's tool calls, their results and
	// its text. Without it the caller gets the final answer only.
	StreamSteps bool
}

// Serve runs the agent over A2A until ctx is cancelled.
func Serve(ctx context.Context, cfg ServeConfig) error {
	base, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return fmt.Errorf("parse public URL %q: %w", cfg.PublicURL, err)
	}

	card := &a2a.AgentCard{
		Name:        cfg.Agent.Name(),
		Description: cfg.Agent.Description(),
		SupportedInterfaces: []*a2a.AgentInterface{{
			URL:             base.JoinPath(InvokePath).String(),
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2a.Version,
		}},
		Version:            cfg.Version,
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             adka2a.BuildAgentSkills(cfg.Agent),
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}

	outputMode := adka2a.OutputArtifactPerRun
	if cfg.StreamSteps {
		outputMode = adka2a.OutputArtifactPerEvent
	}
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        cfg.Agent.Name(),
			Agent:          cfg.Agent,
			SessionService: session.InMemoryService(),
		},
		OutputMode: outputMode,
	})

	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle(InvokePath, a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("serving A2A", "agent", card.Name, "listen", cfg.ListenAddr,
		"card", base.JoinPath(a2asrv.WellKnownAgentCardPath).String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
