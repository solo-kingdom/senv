package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

const mcpSecret = "secret-token-xyz"

func newMCPTestTab(t *testing.T) (*mcpTab, *fakeAuditWriter, string) {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	home := t.TempDir()
	w := &fakeAuditWriter{}
	tab := newMCPTab(Managers{
		Env:         env.NewManager(sm, "pw"),
		Text:        text.NewManager(sm, "pw"),
		MCP:         mcp.NewManager(sm, "pw"),
		MCPHome:     home,
		MCPLedger:   filepath.Join(home, "mcp-exports.json"),
		AuditWriter: w,
	})
	tab.SetSize(120, 24)
	return tab, w, home
}

func loadMCPTab(t *testing.T, tab *mcpTab) *mcpTab {
	t.Helper()
	out := flushTab(tab, tab.load()).(*mcpTab)
	if out.loadErr != "" {
		t.Fatalf("load error: %s", out.loadErr)
	}
	return out
}

func addMCPProfile(t *testing.T, tab *mcpTab, alias, command string, envMap map[string]string) {
	t.Helper()
	err := tab.mgr.MCP.Add(&storage.MCPServerEntry{
		Alias:     alias,
		Transport: storage.MCPTransportStdio,
		Command:   command,
		Args:      []string{"-y", "server-" + alias},
		Env:       envMap,
	})
	if err != nil {
		t.Fatalf("add %s: %v", alias, err)
	}
}

func selectMCPAgent(t *testing.T, tab *mcpTab, id string) {
	t.Helper()
	for i, agent := range tab.agents {
		if agent.ID == id {
			tab.agentIndex = i
			return
		}
	}
	t.Fatalf("agent %s not in Supported()", id)
}

func submitMCPForm(t *testing.T, tab *mcpTab, values map[string]string) *mcpTab {
	t.Helper()
	if tab.form == nil {
		t.Fatal("expected an open form")
	}
	for key, value := range values {
		tab.form.SetValue(key, value)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return flushTab(out, cmd).(*mcpTab)
}

func toastTexts(cmd tea.Cmd) []string {
	var out []string
	for _, msg := range runCmd(cmd) {
		if t, ok := msg.(toastMsg); ok {
			out = append(out, t.text)
		}
	}
	return out
}

func agentConfigPath(t *testing.T, home, id string) string {
	t.Helper()
	target, ok := agentcfg.Find(id)
	if !ok {
		t.Fatalf("unknown agent %s", id)
	}
	return target.ResolveConfigPath(home, "user")
}

func readJSONServerCommand(t *testing.T, path, alias string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("read %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	servers, _ := root["mcpServers"].(map[string]any)
	entry, ok := servers[alias].(map[string]any)
	if !ok {
		return "", false
	}
	cmd, _ := entry["command"].(string)
	return cmd, true
}

func TestMCPTabRegistration(t *testing.T) {
	m := New(Managers{})
	if len(m.tabs) != 3 {
		t.Fatalf("base tabs = %d, want 3", len(m.tabs))
	}
	for _, tab := range m.tabs {
		if tab.Title() == "MCP" {
			t.Fatal("nil MCP manager must not register the tab")
		}
	}

	full := New(newFullManagers(t))
	if len(full.tabs) != 8 {
		t.Fatalf("full tabs = %d, want 8", len(full.tabs))
	}
	titles := make([]string, len(full.tabs))
	for i, tab := range full.tabs {
		titles[i] = tab.Title()
	}
	want := []string{"Env", "Text", "Config", "SSH", "AI", "MCP", "History", "Audit"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Fatalf("tab order = %v, want %v", titles, want)
	}
}

func TestMCPEmptyState(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)
	view := tab.View()
	if !strings.Contains(view, "暂无 MCP Server 档案；按 n 新建") {
		t.Fatalf("empty state missing:\n%s", view)
	}
}

func TestMCPTwoPanesStatusAndFocus(t *testing.T) {
	tab, _, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", map[string]string{"GITHUB_TOKEN": mcpSecret})
	tab = loadMCPTab(t, tab)

	view := tab.View()
	for _, want := range []string{"github", "npx", "1 env", "claude-desktop", "cursor"} {
		if !strings.Contains(view, want) {
			t.Fatalf("browse view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, mcpSecret) {
		t.Fatalf("list leaked env value:\n%s", view)
	}

	selectMCPAgent(t, tab, "cursor")
	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	if tab.mode != mcpModePlan {
		t.Fatalf("mode = %d, want plan", tab.mode)
	}
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)

	if _, ok := readJSONServerCommand(t, agentConfigPath(t, home, "cursor"), "github"); !ok {
		t.Fatal("expected cursor export")
	}
	view = tab.View()
	if !strings.Contains(view, "cursor · 已导出") {
		t.Fatalf("cursor should show 已导出:\n%s", view)
	}
	if !strings.Contains(view, "claude-code · 未导出") {
		t.Fatalf("other agents should stay 未导出:\n%s", view)
	}

	left := tab.serverIndex
	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRight})
	tab = out.(*mcpTab)
	if tab.focusLeft {
		t.Fatal("right arrow should move focus to the agent pane")
	}
	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyDown})
	tab = out.(*mcpTab)
	if tab.serverIndex != left {
		t.Fatal("down in the agent pane must not move the archive cursor")
	}
	if tab.agentIndex == 0 {
		t.Fatal("down in the agent pane should move the agent cursor")
	}
}

func TestMCPDetailMasksLiteralEnv(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", map[string]string{
		"GITHUB_TOKEN": mcpSecret,
		"REF":          "{{env:secrets:GH_TOKEN}}",
	})
	tab = loadMCPTab(t, tab)
	out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*mcpTab)
	view := tab.View()
	for _, want := range []string{"github", "npx", "GITHUB_TOKEN=***", "REF={{env:secrets:GH_TOKEN}}"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, mcpSecret) {
		t.Fatalf("detail leaked env value:\n%s", view)
	}
}

func TestMCPFormCreateEditAndRejects(t *testing.T) {
	tab, w, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)

	out, _ := tab.Update(runeKey("n"))
	tab = out.(*mcpTab)
	if tab.form == nil {
		t.Fatal("n should open the create form")
	}
	if strings.Contains(tab.form.View(), "transport") {
		t.Fatal("transport must not appear in the form")
	}

	tab = submitMCPForm(t, tab, map[string]string{"alias": "github", "command": ""})
	if tab.form == nil {
		t.Fatal("empty command must keep the form open")
	}
	if _, err := tab.mgr.MCP.Get("github"); err == nil {
		t.Fatal("empty command must not create a profile")
	}

	tab = submitMCPForm(t, tab, map[string]string{
		"alias":   "github",
		"command": "npx",
		"args":    "-y\n@modelcontextprotocol/server-github",
		"env":     "GITHUB_TOKEN=" + mcpSecret + "\nREF={{env:secrets:GH_TOKEN}}",
	})
	if tab.form != nil {
		t.Fatalf("create should close the form:\n%s", tab.form.View())
	}
	if len(tab.servers) != 1 || tab.servers[0].Alias != "github" {
		t.Fatalf("servers = %+v", tab.servers)
	}
	if strings.Contains(tab.View(), mcpSecret) {
		t.Fatal("list leaked env after create")
	}
	assertAuditHas(t, w, session.AuditOpMCPServer, "mcp:github", true)
	assertNoSecretLeak(t, w, mcpSecret)

	out, _ = tab.Update(runeKey("n"))
	tab = out.(*mcpTab)
	tab = submitMCPForm(t, tab, map[string]string{"alias": "github", "command": "uvx"})
	if tab.form == nil {
		t.Fatal("duplicate alias must reopen the form")
	}

	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = flushTab(out, cmd).(*mcpTab)
	out, _ = tab.Update(runeKey("e"))
	tab = out.(*mcpTab)
	if tab.form == nil {
		t.Fatal("e should open the edit form")
	}
	if !strings.Contains(tab.form.View(), "GITHUB_TOKEN") {
		t.Fatal("edit form should preview env keys")
	}
	if strings.Contains(tab.form.View(), mcpSecret) {
		t.Fatal("edit form preview leaked env value")
	}
	tab = submitMCPForm(t, tab, map[string]string{"command": "uvx"})
	got, err := tab.mgr.MCP.Get("github")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Alias != "github" || got.Command != "uvx" {
		t.Fatalf("edit changed identity: %+v", got)
	}
}

func TestMCPDeleteDoesNotUnexport(t *testing.T) {
	tab, w, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", map[string]string{"GITHUB_TOKEN": mcpSecret})
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")
	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)
	before, _ := os.ReadFile(agentConfigPath(t, home, "cursor"))

	out, _ = tab.Update(runeKey("d"))
	tab = out.(*mcpTab)
	confirm := tab.View()
	for _, want := range []string{"cursor", "不撤回"} {
		if !strings.Contains(confirm, want) {
			t.Fatalf("delete confirm missing %q:\n%s", want, confirm)
		}
	}
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)
	if _, err := tab.mgr.MCP.Get("github"); err == nil {
		t.Fatal("vault profile should be deleted")
	}
	after, err := os.ReadFile(agentConfigPath(t, home, "cursor"))
	if err != nil {
		t.Fatalf("cursor config disappeared: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("delete mutated agent config:\n%s", after)
	}
	assertAuditHas(t, w, session.AuditOpMCPServer, "mcp:github", true)
}

func TestMCPExportPlanCancelAndCurrentAliasOnly(t *testing.T) {
	tab, _, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", map[string]string{"GITHUB_TOKEN": mcpSecret})
	addMCPProfile(t, tab, "slack", "uvx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")

	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	plan := tab.View()
	if !strings.Contains(plan, "导出计划") || !strings.Contains(plan, "[明文 env]") {
		t.Fatalf("plan missing labels:\n%s", plan)
	}
	if tab.exportPlan == nil || len(tab.exportPlan.Items) != 1 || tab.exportPlan.Items[0].Alias != "github" {
		t.Fatalf("current-agent plan = %+v", tab.exportPlan)
	}
	if strings.Contains(plan, "slack") {
		t.Fatalf("plan must be current alias only:\n%s", plan)
	}
	if strings.Contains(plan, mcpSecret) {
		t.Fatalf("plan leaked env value:\n%s", plan)
	}
	out, cmd = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = flushTab(out, cmd).(*mcpTab)
	if tab.mode != mcpModeNormal {
		t.Fatal("esc should leave the plan")
	}
	if _, err := os.Stat(agentConfigPath(t, home, "cursor")); !os.IsNotExist(err) {
		t.Fatalf("cancel wrote cursor config: %v", err)
	}

	out, cmd = tab.Update(runeKey("X"))
	tab = flushTab(out, cmd).(*mcpTab)
	all := tab.View()
	for _, id := range agentcfg.IDs() {
		if !strings.Contains(all, id) {
			t.Fatalf("all-agent plan missing %s:\n%s", id, all)
		}
	}
	if tab.exportPlan == nil {
		t.Fatal("missing all-agent plan")
	}
	for _, item := range tab.exportPlan.Items {
		if item.Alias != "github" {
			t.Fatalf("all-agent plan included alias %q", item.Alias)
		}
	}
	out, _ = tab.Update(runeKey("n"))
	tab = out.(*mcpTab)
}

func TestMCPExportForceCoversDrift(t *testing.T) {
	tab, w, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")
	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)

	cfgPath := agentConfigPath(t, home, "cursor")
	root, err := agentcfg.ReadJSONRoot(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := agentcfg.Find("cursor")
	agentcfg.SetJSONServer(root, target.JSONServersKey, "github", agentcfg.Server{Command: "local-edit"})
	data, _ := agentcfg.EncodeJSON(root)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	tab = loadMCPTab(t, tab)
	if !strings.Contains(tab.View(), "cursor · 漂移") {
		t.Fatalf("expected drift status:\n%s", tab.View())
	}

	selectMCPAgent(t, tab, "cursor")
	out, cmd = tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	if !strings.Contains(tab.View(), "drift") {
		t.Fatalf("plan should mark drift:\n%s", tab.View())
	}
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)
	if cmd, ok := readJSONServerCommand(t, cfgPath, "github"); !ok || cmd != "local-edit" {
		t.Fatalf("confirm without F overwrote drift: %q", cmd)
	}

	out, cmd = tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, _ = tab.Update(runeKey("F"))
	tab = out.(*mcpTab)
	if !strings.Contains(tab.View(), "强制覆盖") {
		t.Fatalf("F should replan with force:\n%s", tab.View())
	}
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)
	if cmd, ok := readJSONServerCommand(t, cfgPath, "github"); !ok || cmd != "npx" {
		t.Fatalf("force did not restore command: %q ok=%v", cmd, ok)
	}
	assertAuditHas(t, w, session.AuditOpMCPExport, "mcp:github", true)
	assertNoSecretLeak(t, w, mcpSecret)
}

func TestMCPUnexportChangedConfirm(t *testing.T) {
	tab, w, home := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")
	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)

	cfgPath := agentConfigPath(t, home, "cursor")
	root, err := agentcfg.ReadJSONRoot(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := agentcfg.Find("cursor")
	agentcfg.SetJSONServer(root, target.JSONServersKey, "github", agentcfg.Server{Command: "local-edit"})
	data, _ := agentcfg.EncodeJSON(root)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	tab = loadMCPTab(t, tab)
	selectMCPAgent(t, tab, "cursor")
	out, cmd = tab.Update(runeKey("u"))
	tab = flushTab(out, cmd).(*mcpTab)
	out, _ = tab.Update(runeKey("y"))
	tab = out.(*mcpTab)
	if tab.mode != mcpModeChangedConfirm {
		t.Fatalf("mode = %d, want changed confirm", tab.mode)
	}
	if !strings.Contains(tab.View(), "条目已被本地修改") {
		t.Fatalf("changed prompt missing:\n%s", tab.View())
	}
	out, cmd = tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*mcpTab)
	if _, ok := readJSONServerCommand(t, cfgPath, "github"); ok {
		t.Fatal("confirmed changed unexport should remove the entry")
	}
	assertAuditHas(t, w, session.AuditOpMCPExport, "mcp:github", true)
}

func TestMCPExportRequiresSelection(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab = loadMCPTab(t, tab)
	_, cmd := tab.Update(runeKey("x"))
	texts := toastTexts(cmd)
	if len(texts) == 0 || !strings.Contains(texts[0], "没有可导出的档案") {
		t.Fatalf("empty export toast = %v", texts)
	}
	_, cmd = tab.Update(runeKey("u"))
	texts = toastTexts(cmd)
	if len(texts) == 0 || !strings.Contains(texts[0], "没有可撤回的档案") {
		t.Fatalf("empty unexport toast = %v", texts)
	}
}

func TestMCPEnvPreviewHidesValues(t *testing.T) {
	got := mcpEnvPreview("GITHUB_TOKEN=" + mcpSecret + "\nREF={{env:secrets:GH_TOKEN}}")
	if !strings.Contains(got, "GITHUB_TOKEN") || !strings.Contains(got, "REF") {
		t.Fatalf("preview missing keys: %q", got)
	}
	if strings.Contains(got, mcpSecret) || strings.Contains(got, "{{env:") {
		t.Fatalf("preview must not echo values: %q", got)
	}
}

func assertAuditHas(t *testing.T, w *fakeAuditWriter, event session.AuditEventType, targetPart string, success bool) {
	t.Helper()
	for _, c := range w.calls {
		if c.event == event && strings.Contains(c.target, targetPart) && c.success == success {
			if strings.Contains(c.target, mcpSecret) || strings.Contains(c.detail, mcpSecret) {
				t.Fatalf("audit leaked secret: %+v", c)
			}
			return
		}
	}
	t.Fatalf("missing audit %s %q success=%v in %+v", event, targetPart, success, w.calls)
}
