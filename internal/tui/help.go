package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// globalKeys 是顶层 model 处理的键，对每个 Tab 生效；Tab 专属键位来自该
// Tab 的 Bindings()（keymap 注册表），因此总览与实际行为同源、不可能漂移。
var globalKeys = []KeyAction{
	{[]string{"1–9"}, "直达对应 Tab"},
	{[]string{"Tab / Shift+Tab"}, "循环切换 Tab"},
	{[]string{"S"}, "全局搜索"},
	{[]string{"ctrl+r"}, "刷新当前 Tab"},
	{[]string{"?"}, "键位总览"},
	{[]string{"esc"}, "回上一层：清过滤 / 关弹层 / 向导回退"},
	{[]string{"q / ctrl+c"}, "退出（q 仍有待推送时先提示一次）"},
}

// helpTab is the keybinding overview overlay (triggered by `?`).
//
// It renders in place of the tab content, like the search overlay: while open
// it captures every key, and `?`/`esc` close it.
type helpTab struct {
	title    string
	bindings []KeyAction
	width    int
	height   int
}

// helpCloseMsg asks the top-level model to close the help overlay.
type helpCloseMsg struct{}

// newHelpTab builds the overlay for a tab. Tab-specific rows come straight
// from the tab's keymap bindings, so the overlay and the status bar cannot
// drift from what the tab actually handles.
func newHelpTab(title string, tab Tab) *helpTab {
	return &helpTab{title: title, bindings: tab.Bindings()}
}

func (h *helpTab) Title() string         { return "Help" }
func (h *helpTab) Bindings() []KeyAction { return nil }
func (h *helpTab) InputMode() bool       { return true }
func (h *helpTab) Init() tea.Cmd         { return nil }
func (h *helpTab) Reload() tea.Cmd       { return nil }
func (h *helpTab) SetSize(w, hh int)     { h.width, h.height = w, hh }

func (h *helpTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return h, nil
	}
	switch key.String() {
	case "?", "esc":
		return h, func() tea.Msg { return helpCloseMsg{} }
	}
	return h, nil
}

func (h *helpTab) View() string {
	heading := lipgloss.NewStyle().Bold(true)
	rows := []string{heading.Render("全局")}
	for _, b := range globalKeys {
		rows = append(rows, renderBinding(b))
	}
	if h.title != "" {
		rows = append(rows, "", heading.Render(h.title))
		if len(h.bindings) == 0 {
			rows = append(rows, "  （无 Tab 专属键位）")
		}
		for _, b := range h.bindings {
			rows = append(rows, renderBinding(b))
		}
	}
	rows = append(rows, "", statusBarStyle.Render("? / esc 关闭"))
	box := searchOverlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	return clipLines(box, h.height-frameRows)
}

// renderBinding renders one key/desc row with a fixed key column so the
// descriptions line up.
func renderBinding(b KeyAction) string {
	return fmt.Sprintf("  %-18s %s", strings.Join(b.Keys, "/"), b.Desc)
}

// Compile-time guard.
var _ Tab = (*helpTab)(nil)
