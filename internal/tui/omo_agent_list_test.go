package tui

import (
	"strings"
	"testing"

	"github.com/wii/senv/internal/llm"
)

// Both TUI agent lists are registry driven, so adding omo to the two
// registries must surface it in the MCP tab and the LLM tab with no UI code
// change (ADR-0032). This locks that contract.
func TestTUIAgentListsIncludeOmO(t *testing.T) {
	tab := newMCPTab(Managers{})
	found := false
	for _, target := range tab.agents {
		if target.ID == "omo" {
			found = true
		}
		if target.ID == "oh-my-openagent" {
			t.Fatalf("MCP tab must show canonical ids only, got alias %q", target.ID)
		}
	}
	if !found {
		t.Fatal("MCP tab agent list is missing omo")
	}

	rows, _, err := llm.NewSwitchManager(nil, "", t.TempDir()).Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	found = false
	for _, row := range rows {
		if row.AgentID == "omo" || row.AgentName == "OmO" {
			found = true
		}
		if strings.EqualFold(row.AgentID, "oh-my-openagent") {
			t.Fatalf("LLM tab must show canonical ids only, got alias %q", row.AgentID)
		}
	}
	if !found {
		t.Fatal("LLM tab agent list is missing omo")
	}
}
