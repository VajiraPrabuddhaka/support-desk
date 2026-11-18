package agentkit

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"google.golang.org/adk/v2/agent/remoteagent/v2"
)

// AgentCardProvider fetches the agent card at baseURL and checks that every
// endpoint in it stays on baseURL's origin, so a card cannot send calls (and
// the credentials they will carry later) somewhere else.
//
// ADK's own remoteagent.NewAgentCardProvider does the same check and also
// demands https for any host that is not loopback. Inside Docker the agents
// reach each other as http://billing-agent:8002 on a private network, so
// allowHTTP relaxes only that second rule. Leave it off anywhere else.
func AgentCardProvider(baseURL string, allowHTTP bool) remoteagent.AgentCardProvider {
	return func(ctx context.Context) (*a2a.AgentCard, error) {
		base, err := url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse agent URL %q: %w", baseURL, err)
		}
		card, err := agentcard.DefaultResolver.Resolve(ctx, baseURL)
		if err != nil {
			return nil, fmt.Errorf("fetch agent card from %s: %w", baseURL, err)
		}
		for _, iface := range card.SupportedInterfaces {
			if iface == nil {
				continue
			}
			u, err := url.Parse(iface.URL)
			if err != nil {
				return nil, fmt.Errorf("agent card of %s has a bad endpoint %q: %w", baseURL, iface.URL, err)
			}
			if origin(u) != origin(base) {
				return nil, fmt.Errorf("agent card of %s points to %q, outside %s", baseURL, iface.URL, origin(base))
			}
			if u.Scheme != "https" && !isLoopback(u.Hostname()) {
				if !allowHTTP {
					return nil, fmt.Errorf("agent card of %s uses plain http (%q); use https or set A2A_ALLOW_HTTP=true", baseURL, iface.URL)
				}
				slog.Debug("calling agent over plain http", "endpoint", iface.URL)
			}
		}
		return card, nil
	}
}

func origin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	return u.Scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

func isLoopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
