package kit

import (
	"context"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/agent/remoteagent/v2"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
)

// RemoteAgent returns a tool that asks another agent on kagent over A2A. agentURL
// is that agent's endpoint on kagent's gateway, <gateway>/agents/<namespace>/<name>,
// as the AgentType hands it over.
//
// The call carries no credentials, which kagent's gateway accepts in its
// insecure mode only.
func RemoteAgent(name, description, agentURL string) (tool.Tool, error) {
	// A URL with a path is fetched as it is, so name the card itself.
	cardURL := strings.TrimRight(agentURL, "/") + a2asrv.WellKnownAgentCardPath
	remote, err := remoteagent.NewA2A(remoteagent.A2AConfig{
		Name:        name,
		Description: description,
		AgentCardProvider: func(ctx context.Context) (*a2a.AgentCard, error) {
			return agentcard.DefaultResolver.Resolve(ctx, cardURL)
		},
		ClientProvider: remoteagent.NewA2AClientProvider(remoteClientFactory()),
	})
	if err != nil {
		return nil, err
	}
	return agenttool.New(remote, nil), nil
}

// remoteClientFactory makes A2A clients that do not pass this agent's own
// tenant on.
//
// kagent's gateway calls an agent with the tenant <namespace>/<name>, and the
// A2A library puts it on the context of the call being served. A client made in
// that call sends it along by default. The gateway picks the other agent by its
// URL and refuses a call whose tenant names a different one ("tenant does not
// match the Agent URL"), so the hand-off would never arrive.
func remoteClientFactory() *a2aclient.Factory {
	return a2aclient.NewFactory(a2aclient.WithConfig(a2aclient.Config{DisableTenantPropagation: true}))
}
