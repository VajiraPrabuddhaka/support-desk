package agentkit

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

// HeaderSource returns the headers for one outgoing MCP request. It runs on
// every request, so a credential that changes on disk is used without a
// restart.
type HeaderSource func() (http.Header, error)

// StaticHeader sends one header with a fixed value.
func StaticHeader(name, value string) HeaderSource {
	return func() (http.Header, error) {
		h := http.Header{}
		h.Set(name, value)
		return h, nil
	}
}

// BearerTokenFile reads a token from path on every request and sends it as
// "Authorization: Bearer <token>".
func BearerTokenFile(path string) HeaderSource {
	return func() (http.Header, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read token file: %w", err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return nil, fmt.Errorf("token file %s is empty", path)
		}
		h := http.Header{}
		h.Set("Authorization", "Bearer "+token)
		return h, nil
	}
}

type headerRoundTripper struct {
	base   http.RoundTripper
	source HeaderSource
}

func (t headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	headers, err := t.source()
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	for name, values := range headers {
		req.Header[name] = values
	}
	return t.base.RoundTrip(req)
}

// MCPToolset connects to a streamable HTTP MCP server. Every request carries
// the headers from headers, and the model sees only the tools named in
// allowed.
func MCPToolset(endpoint string, headers HeaderSource, allowed []string) (tool.Toolset, error) {
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint,
		HTTPClient: &http.Client{
			Transport: headerRoundTripper{base: http.DefaultTransport, source: headers},
		},
	}
	return mcptoolset.New(mcptoolset.Config{
		Transport:  transport,
		ToolFilter: tool.StringPredicate(allowed),
	})
}
