package agentkit

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

// MCPToolset connects to a streamable HTTP MCP server, and the model sees only
// the tools named in allowed. The agent sends no credentials: the MCP server
// holds its own and acts with them.
func MCPToolset(endpoint string, allowed []string) (tool.Toolset, error) {
	return mcptoolset.New(mcptoolset.Config{
		Transport:  &mcp.StreamableClientTransport{Endpoint: endpoint},
		ToolFilter: tool.StringPredicate(allowed),
	})
}
