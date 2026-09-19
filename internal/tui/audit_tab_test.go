package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/session"
)

type fakeAuditSource struct {
	rows    []session.AuditEntry
	skipped int
	err     error
}

func (f *fakeAuditSource) LoadAuditEvents() ([]session.AuditEntry, int, error) {
	return f.rows, f.skipped, f.err
}

func sampleAuditRows() []session.AuditEntry {
	base := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	return []session.AuditEntry{
		{Timestamp: base.Add(time.Minute), EventType: session.AuditOpEnv, Target: "env:deploy:KEY", Success: true},
		{Timestamp: base, EventType: session.AuditAuthFailure, Target: "", Success: false},
	}
}

func TestAuditTabRendersDatesAndOutcomes(t *testing.T) {
	var tab Tab = newAuditTab(&fakeAuditSource{rows: sampleAuditRows()}, nil)
	tab.SetSize(80, 20)
	tab, _ = tab.Update(drainCmd(t, tab.Init()))
	view := tab.View()
	if !strings.Contains(view, "2026-09-06") || !strings.Contains(view, "op_env") || !strings.Contains(view, "✓") || !strings.Contains(view, "✗") {
		t.Errorf("audit view should render dates/types/outcomes, got %q", view)
	}
}

func TestAuditTabFilterCycles(t *testing.T) {
	src := &fakeAuditSource{rows: sampleAuditRows()}
	var tab Tab = newAuditTab(src, nil)
	tab.SetSize(80, 20)
	tab, _ = tab.Update(drainCmd(t, tab.Init()))

	// 过滤「操作」：只剩 op_env
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if view := tab.View(); !strings.Contains(view, "op_env") || strings.Contains(view, "auth_failure") {
		t.Errorf("op filter should hide session events, got %q", view)
	}
	// 过滤「会话」：只剩 auth_failure
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if view := tab.View(); strings.Contains(view, "op_env") || !strings.Contains(view, "auth_failure") {
		t.Errorf("session filter should hide op events, got %q", view)
	}
	// 新增的「自上次 pull」预设：sync 为 nil（从未 pull）时给出明确空态
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if view := tab.View(); !strings.Contains(view, "no pull yet") {
		t.Errorf("since-pull filter without a pull should show the never-pulled empty state, got %q", view)
	}
	// 再按回到全部
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if view := tab.View(); !strings.Contains(view, "op_env") || !strings.Contains(view, "auth_failure") {
		t.Errorf("all filter should show everything, got %q", view)
	}
}

func TestAuditTabSkippedLinesAndError(t *testing.T) {
	var tab Tab = newAuditTab(&fakeAuditSource{rows: sampleAuditRows(), skipped: 2}, nil)
	tab.SetSize(80, 20)
	tab, _ = tab.Update(drainCmd(t, tab.Init()))
	if view := tab.View(); !strings.Contains(view, "skipped 2 unparseable records") {
		t.Errorf("view should report skipped lines, got %q", view)
	}

	var errTab Tab = newAuditTab(&fakeAuditSource{err: fmt.Errorf("boom")}, nil)
	errTab.SetSize(80, 20)
	errTab, _ = errTab.Update(drainCmd(t, errTab.Init()))
	if view := errTab.View(); !strings.Contains(view, "failed to load audit log") {
		t.Errorf("view should show load error, got %q", view)
	}
}

func TestAuditTabRegisteredOnlyWithSource(t *testing.T) {
	if got := len(New(Managers{}).tabs); got != 4 {
		t.Fatalf("tabs without source = %d, want 4", got)
	}
	if got := len(New(Managers{Audit: &fakeAuditSource{}}).tabs); got != 5 {
		t.Fatalf("tabs with audit source = %d, want 4", got)
	}
}

func TestAuditTabFreeTextFilter(t *testing.T) {
	src := &fakeAuditSource{rows: sampleAuditRows()}
	var tab Tab = newAuditTab(src, nil)
	tab.SetSize(80, 20)
	tab, _ = tab.Update(drainCmd(t, tab.Init()))

	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	tab, _ = tab.Update(key("/"))
	if !tab.InputMode() {
		t.Fatal("expected the audit tab to be in filter input mode after /")
	}
	for _, ch := range []string{"d", "e", "p", "l", "o", "y"} {
		tab, _ = tab.Update(key(ch))
	}
	view := tab.View()
	if !strings.Contains(view, "op_env") || strings.Contains(view, "auth_failure") {
		t.Errorf("text filter should keep only matching targets, got %q", view)
	}
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tab.InputMode() {
		t.Error("enter should leave filter input mode")
	}
	// Filter stays applied after enter.
	if view := tab.View(); strings.Contains(view, "auth_failure") {
		t.Errorf("filter should persist after enter, got %q", view)
	}

	// esc clears the text filter and restores every row.
	tab, _ = tab.Update(key("/"))
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if view := tab.View(); !strings.Contains(view, "auth_failure") {
		t.Errorf("esc should clear the text filter, got %q", view)
	}
}

func TestAuditTabFreeTextFilterNoMatch(t *testing.T) {
	var tab Tab = newAuditTab(&fakeAuditSource{rows: sampleAuditRows()}, nil)
	tab.SetSize(80, 20)
	tab, _ = tab.Update(drainCmd(t, tab.Init()))
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, ch := range []string{"z", "z", "z"} {
		tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(ch)})
	}
	if view := tab.View(); !strings.Contains(view, "no events matching") {
		t.Errorf("view should show a no-match state, got %q", view)
	}
}

// TestAuditTabPaneFillsContentArea 校验面板几何：加载/错误/空/列表四态均撑满
// 内容区，resize 跟随重排（tui-tab-consistency-render）。
func TestAuditTabPaneFillsContentArea(t *testing.T) {
	// 加载态：不再是裸文本
	loading := newAuditTab(&fakeAuditSource{}, nil)
	loading.SetSize(78, 17)
	out := loading.View()
	if !strings.Contains(out, "loading audit log…") {
		t.Fatalf("loading hint missing: %q", clipRunesT(out, 80))
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("loading pane size = %dx%d, want 78x19", w, h)
	}

	// 错误态：内嵌面板、撑满
	errTab := newAuditTab(&fakeAuditSource{err: fmt.Errorf("permission denied")}, nil)
	errTab.SetSize(78, 17)
	errTab.Update(drainCmd(t, errTab.Init()))
	out = errTab.View()
	if !strings.Contains(out, "permission denied") {
		t.Fatalf("error text missing: %q", clipRunesT(out, 80))
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("error pane size = %dx%d, want 78x19", w, h)
	}

	// 空态与列表态：撑满；resize 跟随
	src := &fakeAuditSource{rows: sampleAuditRows()}
	var tab Tab = newAuditTab(src, nil)
	tab.SetSize(78, 17)
	tab, _ = tab.Update(drainCmd(t, tab.Init()))
	out = tab.View()
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("list pane size = %dx%d, want 78x19", w, h)
	}

	// 最小终端冒烟：内容区高度为 0 时不得外溢
	tab.SetSize(28, 0)
	if out = tab.View(); out != "" {
		t.Fatalf("zero-height content area should render nothing, got %q", clipRunesT(out, 60))
	}

	tab.SetSize(60, 12)
	out = tab.View()
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 60 || h != 14 {
		t.Fatalf("after resize pane size = %dx%d, want 60x14", w, h)
	}
}

// TestAuditTabSincePullFilter：自上次 pull 预设只保留 pull 之后的事件；
// 后台 pull 完成（Reload）后 lastPull 刷新、pull 事件进入视图；从未 pull
// 时为明确的空态。
func TestAuditTabSincePullFilter(t *testing.T) {
	pullAt := time.Date(2026, 9, 12, 10, 0, 0, 0, time.Local)
	src := &fakeAuditSource{rows: []session.AuditEntry{
		{Timestamp: pullAt.Add(-time.Hour), EventType: session.AuditOpSync, Target: "vault:main", Success: true, Message: "old"},
		{Timestamp: pullAt.Add(time.Minute), EventType: session.AuditOpSync, Target: "vault:main", Success: true, Message: "pull 3 条"},
		{Timestamp: pullAt.Add(2 * time.Minute), EventType: session.AuditOpLLMSwitch, Target: "agent:codex", Success: true, Message: "switch"},
	}}
	sync := &fakeSyncSource{state: SyncState{LastPull: pullAt, Last: pullAt}}
	var tab Tab = newAuditTab(src, sync)
	tab.SetSize(80, 20)
	tab, _ = tab.Update(drainCmd(t, tab.Init()))

	// 循环到 "since pull" 预设（all → ops → sessions → since pull）
	for i := 0; i < 3; i++ {
		tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	}
	view := tab.View()
	if !strings.Contains(view, "since pull") || !strings.Contains(view, "agent:codex") || !strings.Contains(view, "10:02:00") {
		t.Errorf("since-pull view should contain post-pull events only, got %q", view)
	}
	if strings.Contains(view, "10:00:00") {
		t.Errorf("since-pull view must hide pre-pull events, got %q", view)
	}

	// 后台 pull 落地：sync 上报新的 LastPull，Reload 后旧事件被排除
	newPull := pullAt.Add(time.Hour)
	sync.state = SyncState{LastPull: newPull, Last: newPull}
	src.rows = append(src.rows, session.AuditEntry{
		Timestamp: newPull.Add(time.Minute), EventType: session.AuditOpSync, Target: "vault:main", Success: true, Message: "fresh pull",
	})
	tab, _ = tab.Update(drainCmd(t, tab.Reload()))
	view = tab.View()
	if !strings.Contains(view, "11:01:00") {
		t.Errorf("reloaded view should contain the fresh pull event, got %q", view)
	}
	if strings.Contains(view, "10:02:00") || strings.Contains(view, "agent:codex") {
		t.Errorf("reloaded view should exclude events before the new pull, got %q", view)
	}
}
