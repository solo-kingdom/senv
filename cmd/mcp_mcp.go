package cmd

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpServerView is the explicit MCP response whitelist for stored profiles:
// identity and shape only. Env values would carry tokens, and even env key
// names hint at credentials, so neither is exposed to a calling agent.
type mcpServerView struct {
	Alias       string `json:"alias"`
	Transport   string `json:"transport"`
	Description string `json:"description,omitempty"`
}

func (m *managers) mcpServerList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	servers, err := m.mcpServer.List()
	if err != nil {
		return errResult(err)
	}
	out := make([]mcpServerView, 0, len(servers))
	for _, server := range servers {
		out = append(out, mcpServerView{
			Alias:       server.Alias,
			Transport:   server.Transport,
			Description: server.Description,
		})
	}
	return textResult(out)
}
