package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/agentcfg"
)

func TestMCPScopeToggleUpdatesTitleAndStatus(t *testing.T) {
	tab, _, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")

	if !strings.Contains(tab.View(), "scope: user") {
		t.Fatalf("default title must show user scope:\n%s", tab.View())
	}

	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)
	tab = loadMCPTab(t, tab)
	if !strings.Contains(tab.View(), "cursor · exported") {
		t.Fatalf("user-scope export must show exported:\n%s", tab.View())
	}

	out, _ = tab.Update(runeKey("s"))
	tab = out.(*mcpTab)
	if tab.scope != "project" {
		t.Fatalf("s must switch to project, got %q", tab.scope)
	}
	view := tab.View()
	if !strings.Contains(view, "scope: project") {
		t.Fatalf("title missing project scope:\n%s", view)
	}
	if !strings.Contains(view, "cursor · not exported") {
		t.Fatalf("project scope must recompute status (user export is a different file):\n%s", view)
	}

	out, _ = tab.Update(runeKey("s"))
	tab = out.(*mcpTab)
	if tab.scopeOrDefault() != "user" {
		t.Fatalf("second s must return to user, got %q", tab.scope)
	}
	if !strings.Contains(tab.View(), "scope: user") || !strings.Contains(tab.View(), "cursor · exported") {
		t.Fatalf("back to user must restore exported status:\n%s", tab.View())
	}
	if _, err := os.Stat(agentConfigPath(t, home, "cursor")); err != nil {
		t.Fatalf("user-level cursor config missing: %v", err)
	}
}

func TestMCPScopeBindingsIncludeS(t *testing.T) {
	tab := newMCPTab(Managers{})
	set := registeredKeySet(tab.Bindings())
	if !set["s"] {
		t.Fatalf("Bindings missing s: %v", set)
	}
}

func TestMCPProjectScopeExportWritesCursorProjectFile(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	tab, _, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")

	out, _ := tab.Update(runeKey("s"))
	tab = out.(*mcpTab)

	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	if tab.exportPlan == nil || len(tab.exportPlan.Items) != 1 {
		t.Fatalf("export plan = %+v", tab.exportPlan)
	}
	item := tab.exportPlan.Items[0]
	if item.Path != ".cursor/mcp.json" {
		t.Fatalf("project cursor path = %q, want .cursor/mcp.json", item.Path)
	}

	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)

	project := filepath.Join(cwd, ".cursor", "mcp.json")
	if _, err := os.Stat(project); err != nil {
		t.Fatalf("project export must write %s: %v", project, err)
	}
	userPath := agentConfigPath(t, home, "cursor")
	if _, err := os.Stat(userPath); !os.IsNotExist(err) {
		t.Fatalf("project export must not create user-level file %s: %v", userPath, err)
	}
	if _, err := os.Stat(tab.mgr.MCPLedger); err != nil {
		t.Fatalf("ledger must be written: %v", err)
	}
}

func TestMCPUserScopeExportPathUnchanged(t *testing.T) {
	tab, _, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")

	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	want := agentConfigPath(t, home, "cursor")
	if tab.exportPlan == nil || tab.exportPlan.Items[0].Path != want {
		t.Fatalf("user cursor path = %q, want %q", tab.exportPlan.Items[0].Path, want)
	}
}

func TestMCPProjectScopeUnexportUsesSamePath(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	tab, _, _ := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")

	out, _ := tab.Update(runeKey("s"))
	tab = out.(*mcpTab)
	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)

	project := filepath.Join(cwd, ".cursor", "mcp.json")
	if _, err := os.Stat(project); err != nil {
		t.Fatal(err)
	}

	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")
	out, cmd = tab.Update(runeKey("u"))
	tab = flushTab(out, cmd).(*mcpTab)
	if tab.unexportPlan == nil || len(tab.unexportPlan.Items) == 0 {
		t.Fatalf("unexport plan empty: %+v", tab.unexportPlan)
	}
	if tab.unexportPlan.Items[0].Path != ".cursor/mcp.json" {
		t.Fatalf("unexport path = %q", tab.unexportPlan.Items[0].Path)
	}
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)
	if _, ok := readJSONServerCommand(t, project, "github"); ok {
		t.Fatal("project unexport must remove the github entry")
	}
}

func TestMCPUserScopeUnexportPathUnchanged(t *testing.T) {
	tab, _, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")

	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)

	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")
	out, cmd = tab.Update(runeKey("u"))
	tab = flushTab(out, cmd).(*mcpTab)
	want := agentConfigPath(t, home, "cursor")
	if tab.unexportPlan == nil || tab.unexportPlan.Items[0].Path != want {
		t.Fatalf("user cursor unexport path = %q, want %q", tab.unexportPlan.Items[0].Path, want)
	}
}

func TestMCPProjectScopeLeavesOtherAgentsUnchanged(t *testing.T) {
	tab, _, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "codex")

	out, _ := tab.Update(runeKey("s"))
	tab = out.(*mcpTab)
	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	codex, ok := agentcfg.Find("codex")
	if !ok {
		t.Fatal("codex missing from Supported()")
	}
	want := codex.ResolveConfigPath(home, "project")
	user := codex.ResolveConfigPath(home, "user")
	if want != user {
		t.Fatalf("codex should ignore scope: project=%q user=%q", want, user)
	}
	if tab.exportPlan == nil || tab.exportPlan.Items[0].Path != want {
		t.Fatalf("codex path = %q, want %q", tab.exportPlan.Items[0].Path, want)
	}
}
