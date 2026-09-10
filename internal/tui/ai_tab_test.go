package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
)

func newAITestTab(t *testing.T) (*aiTab, string) {
	t.Helper()
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	pm := llm.NewProviderManager(store, "test-password")
	if _, err := pm.AddProvider(llm.AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-tui-secret", Models: []string{"m1", "m2"}, DefaultModel: "m1",
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	home := t.TempDir()
	mgr := Managers{LLM: pm, LLMPointer: llm.DefaultPointerPath(home), LLMHome: home}
	tab := newAITab(mgr)
	tab.SetSize(100, 24)
	return tab, home
}

func runAITabLoad(t *testing.T, tab *aiTab) {
	t.Helper()
	cmd := tab.load()
	msg := cmd()
	next, _ := tab.Update(msg)
	if next.(*aiTab) != tab {
		t.Fatal("Update returned a different tab pointer")
	}
	if tab.loadErr != "" {
		t.Fatalf("load error: %s", tab.loadErr)
	}
}

func TestAITabRegistration(t *testing.T) {
	if tabs := New(Managers{}); len(tabs.tabs) != 3 {
		t.Fatalf("base tab count = %d, want 3 (no AI without LLM)", len(tabs.tabs))
	}
	m := New(Managers{LLM: &llm.ProviderManager{}})
	last := m.tabs[len(m.tabs)-1]
	if last.Title() != "AI" {
		t.Fatalf("last tab = %q, want AI", last.Title())
	}
}

func TestAITabBrowseNoSecretLeak(t *testing.T) {
	tab, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	view := tab.View()
	for _, want := range []string{"main", "https://api.example.com", "text:llm-keys/main", "未切换", "不支持", "zcode", "cursor"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "sk-tui-secret") {
		t.Fatal("view leaked credential plaintext")
	}
}

func TestAITabEmptyState(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatal(err)
	}
	tab := newAITab(Managers{LLM: llm.NewProviderManager(store, "test-password"), LLMHome: t.TempDir()})
	tab.SetSize(80, 20)
	runAITabLoad(t, tab)
	if view := tab.View(); !strings.Contains(view, "senv ai provider add") {
		t.Fatalf("empty state hint missing:\n%s", view)
	}
}

// driveAITabSwitch 走完 agent→model→confirm 选择流并返回 switch 结果 msg。
func driveAITabSwitch(t *testing.T, tab *aiTab, agentSteps int) tea.Msg {
	t.Helper()
	if _, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}); cmd != nil {
		t.Fatal("flow start unexpectedly returned a command")
	}
	if tab.flow != aiFlowSelectAgent {
		t.Fatalf("flow = %v, want selectAgent", tab.flow)
	}
	for i := 0; i < agentSteps; i++ {
		tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tab.flow != aiFlowSelectModel {
		t.Fatalf("flow = %v, want selectModel", tab.flow)
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // 确认默认选中模型
	if tab.flow != aiFlowConfirm {
		t.Fatalf("flow = %v, want confirm", tab.flow)
	}
	_, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("confirm did not return switch command")
	}
	return cmd()
}

func TestAITabSwitchSuccess(t *testing.T) {
	tab, home := newAITestTab(t)
	runAITabLoad(t, tab)

	msg := driveAITabSwitch(t, tab, 0) // claude-code 是第一个 agent
	result, ok := msg.(aiSwitchResultMsg)
	if !ok || result.err != nil {
		t.Fatalf("switch msg = %#v", msg)
	}
	next, cmd := tab.Update(result)
	tab = next.(*aiTab)
	if !strings.Contains(tab.notice, "Claude Code → main") {
		t.Fatalf("notice = %q", tab.notice)
	}
	if cmd == nil {
		t.Fatal("success did not trigger reload")
	}
	next, _ = tab.Update(cmd())
	tab = next.(*aiTab)
	if !strings.Contains(tab.View(), "claude-code") || !strings.Contains(tab.View(), "main / m1") {
		t.Fatalf("pointer not refreshed:\n%s", tab.View())
	}

	// 配置文件真实写回（复用 SwitchManager）。
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(raw), "sk-tui-secret") {
		t.Fatalf("settings.json = %s, %v", raw, err)
	}
}

func TestAITabSwitchFailureBanner(t *testing.T) {
	tab, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	// 指针路径挂在普通文件下使保存必败。
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tab.mgr.LLMPointer = filepath.Join(blocker, "pointers.json")

	msg := driveAITabSwitch(t, tab, 0)
	result := msg.(aiSwitchResultMsg)
	if result.err == nil {
		t.Fatal("switch unexpectedly succeeded")
	}
	_, cmd := tab.Update(result)
	if cmd == nil {
		t.Fatal("failure did not produce error banner command")
	}
	banner := cmd()
	if _, ok := banner.(errMsg); !ok {
		t.Fatalf("banner msg = %#v", banner)
	}
}

func TestAITabSwitchCodexGuidance(t *testing.T) {
	tab, home := newAITestTab(t)
	runAITabLoad(t, tab)

	msg := driveAITabSwitch(t, tab, 1) // 第二个 agent 是 codex
	result := msg.(aiSwitchResultMsg)
	if result.err != nil {
		t.Fatalf("switch error: %v", result.err)
	}
	next, _ := tab.Update(result)
	tab = next.(*aiTab)
	if !strings.Contains(tab.notice, "SENV_MAIN_API_KEY") {
		t.Fatalf("codex guidance missing: %q", tab.notice)
	}
	// codex 配置不落密钥。
	raw, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-tui-secret") {
		t.Fatal("codex config leaked credential")
	}
}

func TestAITabFlowEscape(t *testing.T) {
	tab, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !tab.InputMode() {
		t.Fatal("flow should enable InputMode")
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tab.flow != aiFlowNone || tab.InputMode() {
		t.Fatalf("esc did not cancel flow: %v", tab.flow)
	}
}

func TestAITabLoadError(t *testing.T) {
	tab, _ := newAITestTab(t)
	next, cmd := tab.Update(aiLoadedMsg{err: errors.New("boom")})
	tab = next.(*aiTab)
	if cmd != nil || tab.loadErr == "" {
		t.Fatalf("loadErr = %q, cmd = %v", tab.loadErr, cmd)
	}
	if !strings.Contains(tab.View(), "boom") {
		t.Fatalf("view missing error:\n%s", tab.View())
	}
}
