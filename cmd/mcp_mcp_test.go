package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func setupMCPServerToolTest(t *testing.T) *managers {
	t.Helper()
	newAuditTestProject(t)
	resetMCPAddFlags(t)
	mcpAddCommand = "npx"
	mcpAddEnv = []string{"GITHUB_TOKEN=super-secret-value"}
	runSSHCommand(t, mcpAddCmd.RunE(&cobra.Command{}, []string{"github"}))
	manager, err := getMCPManager()
	if err != nil {
		t.Fatal(err)
	}
	return &managers{mcpServer: manager, autoPull: func() {}}
}

func TestMCPServerListToolWhitelistedOnly(t *testing.T) {
	requestManagers := setupMCPServerToolTest(t)
	res, _, err := requestManagers.mcpServerList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("mcp_server_list = %v, %v", res, err)
	}
	text := textOf(t, res)
	if strings.Contains(text, "super-secret-value") || strings.Contains(text, "GITHUB_TOKEN") {
		t.Fatalf("mcp_server_list leaked profile detail: %s", text)
	}
	var servers []map[string]any
	if err := json.Unmarshal([]byte(text), &servers); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(servers))
	}
	allowed := map[string]bool{"alias": true, "transport": true, "description": true}
	for key := range servers[0] {
		if !allowed[key] {
			t.Fatalf("unexpected response field %q", key)
		}
	}
	if servers[0]["alias"] != "github" || servers[0]["transport"] != "stdio" {
		t.Fatalf("server row = %v", servers[0])
	}
}

func TestMCPServerListToolEmptyArray(t *testing.T) {
	newAuditTestProject(t)
	manager, err := getMCPManager()
	if err != nil {
		t.Fatal(err)
	}
	requestManagers := &managers{mcpServer: manager, autoPull: func() {}}
	res, _, err := requestManagers.mcpServerList(context.Background(), nil, struct{}{})
	if err != nil || res.IsError {
		t.Fatalf("mcp_server_list = %v, %v", res, err)
	}
	if got := strings.TrimSpace(textOf(t, res)); got != "[]" {
		t.Fatalf("empty catalog response = %q, want []", got)
	}
}

func TestMCPToolCatalogueHasNoMCPWriteTools(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range toolCatalogue() {
		names[tool.Name] = true
	}
	if !names["mcp_server_list"] {
		t.Fatalf("mcp_server_list missing from catalogue: %v", names)
	}
	for name := range names {
		if !strings.Contains(name, "mcp_server") {
			continue
		}
		if name != "mcp_server_list" {
			t.Fatalf("unexpected MCP profile tool %q", name)
		}
	}
}
