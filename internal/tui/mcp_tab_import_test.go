package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func writeImportFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openImportForm(t *testing.T, tab *mcpTab) *mcpTab {
	t.Helper()
	out, _ := tab.Update(runeKey("i"))
	tab = out.(*mcpTab)
	if tab.form == nil {
		t.Fatal("i must open the import path form")
	}
	return tab
}

func TestMCPImportJSONReport(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)
	home := t.TempDir()
	t.Setenv("HOME", home)
	body := `{
	  "mcpServers": {
	    "github": {"command": "npx", "args": ["-y", "github"]},
	    "web": {"url": "https://api.example.com/mcp"},
	    "broken": {"headers": {"A": "b"}}
	  }
	}`
	writeImportFile(t, home, ".claude.json", body)

	tab = openImportForm(t, tab)
	tab = submitMCPForm(t, tab, map[string]string{"path": "~/.claude.json"})
	if tab.mode != mcpModeImportReport || tab.importReport == nil {
		t.Fatalf("import must open the report, mode=%v report=%v", tab.mode, tab.importReport != nil)
	}
	rep := tab.importReport
	if rep.created != 2 || rep.conflicts != 0 || rep.failures != 1 {
		t.Fatalf("counts = created=%d conflict=%d failed=%d", rep.created, rep.conflicts, rep.failures)
	}
	view := tab.View()
	for _, want := range []string{"import report", "github", "create", "stdio", "web", "http", "broken", "failed", "neither url nor command", "created 2", "conflict 0", "failed 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("report missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "https://api.example.com") {
		t.Fatalf("report must not contain values:\n%s", view)
	}
	servers, err := tab.mgr.MCP.List()
	if err != nil || len(servers) != 2 {
		t.Fatalf("vault count = %d, %v", len(servers), err)
	}
}

func TestMCPImportTOMLReport(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)
	body := `[mcp_servers.remote]
url = "https://api.example.com/mcp"
transport = "streamable-http"

[mcp_servers.local]
command = "npx"
args = ["-y", "server-x"]
`
	path := writeImportFile(t, t.TempDir(), "config.toml", body)
	tab = openImportForm(t, tab)
	tab = submitMCPForm(t, tab, map[string]string{"path": path})
	if tab.mode != mcpModeImportReport {
		t.Fatalf("mode=%v", tab.mode)
	}
	rep := tab.importReport
	if rep.created != 2 || rep.failures != 0 {
		t.Fatalf("counts = %+v", rep)
	}
	view := tab.View()
	if !strings.Contains(view, "remote") || !strings.Contains(view, "local") || !strings.Contains(view, "created 2") {
		t.Fatalf("toml report missing rows:\n%s", view)
	}
	if _, err := tab.mgr.MCP.Get("remote"); err != nil {
		t.Fatalf("remote missing: %v", err)
	}
}

func TestMCPImportConflictSkipsExisting(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "keep-me", map[string]string{"TOKEN": "orig"})
	tab = loadMCPTab(t, tab)
	body := `{
	  "mcpServers": {
	    "github": {"command": "overwrite", "args": ["nope"]},
	    "fresh": {"command": "npx"}
	  }
	}`
	path := writeImportFile(t, t.TempDir(), "config.json", body)
	tab = openImportForm(t, tab)
	tab = submitMCPForm(t, tab, map[string]string{"path": path})
	if tab.mode != mcpModeImportReport {
		t.Fatalf("mode=%v", tab.mode)
	}
	rep := tab.importReport
	if rep.created != 1 || rep.conflicts != 1 || rep.failures != 0 {
		t.Fatalf("counts = created=%d conflict=%d failed=%d", rep.created, rep.conflicts, rep.failures)
	}
	view := tab.View()
	if !strings.Contains(view, "conflict") || !strings.Contains(view, "already exists") {
		t.Fatalf("conflict row missing:\n%s", view)
	}
	entry, err := tab.mgr.MCP.Get("github")
	if err != nil || entry.Command != "keep-me" || entry.Env["TOKEN"] != "orig" {
		t.Fatalf("existing profile mutated: %+v, %v", entry, err)
	}
	if _, err := tab.mgr.MCP.Get("fresh"); err != nil {
		t.Fatalf("fresh must be created: %v", err)
	}
}

func TestMCPImportBadJSONZeroVaultChange(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)
	path := writeImportFile(t, t.TempDir(), "bad.json", "{not json")
	tab = openImportForm(t, tab)
	tab = submitMCPForm(t, tab, map[string]string{"path": path})
	if tab.mode != mcpModeNormal || tab.importReport != nil {
		t.Fatalf("parse failure must not open report, mode=%v", tab.mode)
	}
	if servers, _ := tab.mgr.MCP.List(); len(servers) != 0 {
		t.Fatalf("vault must stay empty: %v", servers)
	}
}

func TestMCPImportFormEscNoSideEffects(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)
	tab = openImportForm(t, tab)
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = flushTab(out, cmd).(*mcpTab)
	if tab.form != nil {
		t.Fatal("esc must close the form")
	}
	if tab.mode != mcpModeNormal {
		t.Fatalf("esc must return to normal, mode=%v", tab.mode)
	}
	if servers, _ := tab.mgr.MCP.List(); len(servers) != 0 {
		t.Fatalf("cancel must not create profiles: %v", servers)
	}
}

func TestMCPImportReportClosesOnEnter(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)
	path := writeImportFile(t, t.TempDir(), "one.json", `{"mcpServers":{"only":{"command":"npx"}}}`)
	tab = openImportForm(t, tab)
	tab = submitMCPForm(t, tab, map[string]string{"path": path})
	if tab.mode != mcpModeImportReport {
		t.Fatalf("mode=%v", tab.mode)
	}
	out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*mcpTab)
	if tab.mode != mcpModeNormal || tab.importReport != nil {
		t.Fatalf("enter must close report: mode=%v report=%v", tab.mode, tab.importReport != nil)
	}
}
