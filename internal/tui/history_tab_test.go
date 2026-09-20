package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/provider"
)

// fakeHistorySource 可编程的 History 数据源
type fakeHistorySource struct {
	rows     []provider.HistoryVersion
	restored []provider.HistoryVersion
	restoreE error
	// value 非空时 DecryptHistory 一律返回它（用于构造长值/多行值）。
	value string
}

func (f *fakeHistorySource) History(ctx context.Context, flt provider.HistoryFilter) ([]provider.HistoryVersion, error) {
	if flt.Key == "" {
		return f.rows, nil
	}
	var out []provider.HistoryVersion
	for _, r := range f.rows {
		if r.Kind == flt.Kind && r.Grp == flt.Grp && r.Key == flt.Key {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeHistorySource) DecryptHistory(v provider.HistoryVersion) (string, error) {
	if v.Kind == "env" && string(v.Ciphertext) == "bad" {
		return "", errors.New("无法解密")
	}
	if f.value != "" {
		return f.value, nil
	}
	return fmt.Sprintf("value-of-rev-%d", v.Revision), nil
}

func (f *fakeHistorySource) Restore(ctx context.Context, v provider.HistoryVersion) error {
	if f.restoreE != nil {
		return f.restoreE
	}
	f.restored = append(f.restored, v)
	return nil
}

func sampleHistoryRows() []provider.HistoryVersion {
	base := time.Date(2026, 9, 6, 10, 30, 0, 0, time.UTC)
	return []provider.HistoryVersion{
		{Kind: "env", Grp: "deploy", Key: "KEY", Ciphertext: []byte("v2"), Revision: 3, CreatedAt: base.Add(time.Minute)},
		{Kind: "env", Grp: "deploy", Key: "KEY", Ciphertext: []byte("v1"), Revision: 2, CreatedAt: base},
	}
}

func drainCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestHistoryTabRegisteredOnlyWithSource(t *testing.T) {
	if got := len(New(Managers{}).tabs); got != 4 {
		t.Fatalf("tabs without source = %d, want 4", got)
	}
	if got := len(New(Managers{History: &fakeHistorySource{}}).tabs); got != 5 {
		t.Fatalf("tabs with source = %d, want 4", got)
	}
}

func TestHistoryTabBrowseAndRestore(t *testing.T) {
	src := &fakeHistorySource{rows: sampleHistoryRows()}
	var tab Tab = newHistoryTab(src)
	tab.SetSize(80, 20)
	tab.(*historyTab).visited = true // 模拟用户已激活（延迟加载语义）

	// 初始加载（recent 模式）
	msg := drainCmd(t, tab.Init())
	loaded := msg.(historyLoadedMsg)
	if loaded.err != nil {
		t.Fatalf("load: %v", loaded.err)
	}
	tab, _ = tab.Update(loaded)

	view := tab.View()
	if !strings.Contains(view, "2026-09-06") {
		t.Errorf("recent view should show dates, got %q", view)
	}

	// enter 进入单条目历史
	tab, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msg := drainCmd(t, cmd); msg != nil {
		tab, _ = tab.Update(msg)
	}
	ht := tab.(*historyTab)
	if ht.mode != historyModeEntry || !strings.Contains(ht.entryID, "deploy") {
		t.Fatalf("after enter: mode %d entryID %q", ht.mode, ht.entryID)
	}

	// R → 确认模式 → enter 执行恢复（r 已统一为重命名语义）
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if ht.mode != historyModeConfirm {
		t.Fatalf("after R: mode %d", ht.mode)
	}
	tab, cmd = tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msg := drainCmd(t, cmd); msg != nil {
		tab, _ = tab.Update(msg)
	}
	if len(src.restored) != 1 {
		t.Fatalf("restored %d versions, want 1", len(src.restored))
	}
	if src.restored[0].Revision != 3 {
		t.Errorf("restored revision = %d, want 3 (cursor at newest)", src.restored[0].Revision)
	}
}

func TestHistoryTabDecryptFailureShownNotFatal(t *testing.T) {
	src := &fakeHistorySource{rows: []provider.HistoryVersion{
		{Kind: "env", Grp: "g", Key: "K", Ciphertext: []byte("bad"), Revision: 9, CreatedAt: time.Now()},
	}}
	var tab Tab = newHistoryTab(src)
	tab.SetSize(80, 20)
	tab.(*historyTab).visited = true // 模拟用户已激活（延迟加载语义）
	tab, cmd := tab.Update(drainCmd(t, tab.Init()).(historyLoadedMsg))
	_ = cmd
	view := tab.View()
	if !strings.Contains(view, "<cannot decrypt>") {
		t.Errorf("decrypt failure should be shown inline, got %q", view)
	}
}

func TestHistoryTabRestoreErrorReported(t *testing.T) {
	src := &fakeHistorySource{rows: sampleHistoryRows(), restoreE: errors.New("boom")}
	var tab Tab = newHistoryTab(src)
	tab.SetSize(80, 20)
	tab.(*historyTab).visited = true // 模拟用户已激活（延迟加载语义）
	tab, cmd := tab.Update(drainCmd(t, tab.Init()).(historyLoadedMsg))
	_ = cmd
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	tab, cmd = tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := drainCmd(t, cmd)
	if _, ok := msg.(historyRestoredMsg); !ok {
		t.Fatalf("expected historyRestoredMsg, got %T", msg)
	}
	tab, _ = tab.Update(msg)
	// 错误通过 errMsg 冒泡到顶层错误栏；此处不 panic 即可
}

// TestHistoryTabPaneFillsContentArea 校验面板几何：加载/空/列表三态均撑满内容
// 区，与其他 Tab 外框一致（tui-tab-consistency-render）。
func TestHistoryTabPaneFillsContentArea(t *testing.T) {
	newTab := func() *historyTab {
		tab := newHistoryTab(&fakeHistorySource{rows: sampleHistoryRows()})
		tab.SetSize(78, 17)
		tab.visited = true
		return tab
	}

	// 加载态：不再是裸文本，面板与内容区等宽（78），高度 +2 为圆角边框
	tab := newTab()
	out := tab.View()
	if !strings.Contains(out, "loading history…") {
		t.Fatalf("loading hint missing: %q", clipRunesT(out, 80))
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("loading pane size = %dx%d, want 78x19", w, h)
	}

	// 空态：同样撑满
	empty := newHistoryTab(&fakeHistorySource{})
	empty.SetSize(78, 17)
	empty.visited = true
	empty.Update(drainCmd(t, empty.Init()))
	out = empty.View()
	if !strings.Contains(out, "(no history versions:") {
		t.Fatalf("empty hint missing: %q", clipRunesT(out, 80))
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("empty pane size = %dx%d, want 78x19", w, h)
	}

	// 列表态：撑满，且列表多于可视行数时标题带可见区间提示
	many := make([]provider.HistoryVersion, 0, 30)
	base := time.Date(2026, 9, 6, 10, 30, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		many = append(many, provider.HistoryVersion{
			Kind: "env", Grp: "deploy", Key: "KEY",
			Ciphertext: []byte(fmt.Sprintf("v%d", i)), Revision: int64(i + 1),
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	full := newHistoryTab(&fakeHistorySource{rows: many})
	full.SetSize(78, 17)
	full.visited = true
	full.Update(drainCmd(t, full.Init()))
	out = full.View()
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("list pane size = %dx%d, want 78x19", w, h)
	}
	if !strings.Contains(out, " 1–") {
		t.Fatalf("windowed range hint missing in title: %q", clipRunesT(out, 80))
	}

	// 最小终端冒烟：内容区高度为 0 时不得外溢
	full.SetSize(28, 0)
	if out = full.View(); out != "" {
		t.Fatalf("zero-height content area should render nothing, got %q", clipRunesT(out, 60))
	}

	// resize 跟随：新尺寸后面板宽度跟随内容区
	full.SetSize(60, 12)
	out = full.View()
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 60 || h != 14 {
		t.Fatalf("after resize pane size = %dx%d, want 60x14", w, h)
	}
}

// TestHistoryTabDetailOpensScrollableOverlay 校验详情走共享滚动弹层：内容
// 顶替整个面板位、长值不被面板底边裁掉且可滚动到尾部（tui-viewer 规约
// 「完整内容 SHALL 通过 enter 打开的详情弹层查看」）。
func TestHistoryTabDetailOpensScrollableOverlay(t *testing.T) {
	var value strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&value, "line-%02d\n", i)
	}
	var tab Tab = newHistoryTab(&fakeHistorySource{rows: sampleHistoryRows(), value: value.String()})
	tab.SetSize(78, 17)
	tab.(*historyTab).visited = true
	tab, _ = tab.Update(drainCmd(t, tab.Init()))

	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // recent → entry
	out := tab.View()
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("entry pane size = %dx%d, want 78x19", w, h)
	}

	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // entry → detail 弹层
	ht := tab.(*historyTab)
	if ht.detail == nil {
		t.Fatal("enter should open the detail overlay")
	}
	out = tab.View()
	if !strings.Contains(out, "line-01") {
		t.Fatalf("detail overlay should show the value head: %q", clipRunesT(out, 80))
	}
	// 弹层占用与面板完全相同的位置（78×17 内容区 → 78×19 面板槽）。
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("detail overlay size = %dx%d, want 78x19", w, h)
	}

	// 尾部内容初始不可见，PgDn 滚动后可见（旧的内联详情在这里被面板底边裁掉）。
	if strings.Contains(out, "line-60") {
		t.Fatal("tail must not be visible before scrolling")
	}
	for i := 0; i < 10; i++ {
		tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if out = tab.View(); !strings.Contains(out, "line-60") {
		t.Fatalf("scrolling should reveal the value tail: %q", clipRunesT(out, 80))
	}

	// esc 关闭弹层并回到时间线（不再直接回到 recent）。
	tab, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if msg := drainCmd(t, cmd); msg != nil {
		tab, _ = tab.Update(msg)
	}
	if ht.detail != nil {
		t.Fatal("esc should close the overlay")
	}
	if ht.mode != historyModeEntry {
		t.Fatalf("after closing detail: mode %d, want entry", ht.mode)
	}

	tab, _ = tab.Update(runeKey("R")) // entry → confirm
	out = tab.View()
	if !strings.Contains(out, "[y/N]") {
		t.Fatalf("confirm prompt missing: %q", clipRunesT(out, 80))
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("confirm pane size = %dx%d, want 78x19", w, h)
	}
}

// TestHistoryTabRowsFillPaneWidth 校验列表行按面板实际宽度分配列宽：宽终端
// 下预览不再钉死在 30 rune（此前整行只用到约一半宽度就把数据截断）。
func TestHistoryTabRowsFillPaneWidth(t *testing.T) {
	long := strings.Repeat("A", 120)
	var tab Tab = newHistoryTab(&fakeHistorySource{rows: sampleHistoryRows(), value: long})
	tab.SetSize(160, 20)
	tab.(*historyTab).visited = true
	tab, _ = tab.Update(drainCmd(t, tab.Init()))

	out := tab.View()
	// 预览列宽随面板放大：30 rune 之外的内容必须可见。
	if !strings.Contains(out, strings.Repeat("A", 60)) {
		t.Fatalf("preview should use the widened column: %q", clipRunesT(out, 200))
	}
	// 整行铺满面板内容宽（面板 158 列含 2 列边框，行内容 156 列）。
	row := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "AAA") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("data row not found in view")
	}
	if w := lipgloss.Width(row); w < 150 {
		t.Fatalf("row width = %d, want ~156 (fills the pane)", w)
	}
}

// TestHistoryTabDetailStaysInsidePane 校验 confirm/flash 附加行仍渲染在面板
// 内部且面板总高不超出内容区（tui-tab-consistency-render）。
func TestHistoryTabDetailStaysInsidePane(t *testing.T) {
	var tab Tab = newHistoryTab(&fakeHistorySource{rows: sampleHistoryRows()})
	tab.SetSize(78, 17)
	tab.(*historyTab).visited = true
	tab, _ = tab.Update(drainCmd(t, tab.Init()))

	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // recent → entry
	out := tab.View()
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("entry pane size = %dx%d, want 78x19", w, h)
	}

	tab, _ = tab.Update(runeKey("R")) // entry → confirm
	out = tab.View()
	if !strings.Contains(out, "[y/N]") {
		t.Fatalf("confirm prompt missing: %q", clipRunesT(out, 80))
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 78 || h != 19 {
		t.Fatalf("confirm pane size = %dx%d, want 78x19", w, h)
	}
}
