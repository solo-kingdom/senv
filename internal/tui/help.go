package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// KeyBinding is one key/hint row in the help overlay.
type KeyBinding struct {
	Keys string
	Desc string
}

// globalKeys are handled by the top-level model, so they are shown for every
// tab. Tab-specific rows come from the active tab's Help() string.
var globalKeys = []KeyBinding{
	{"1–9", "直达对应 Tab"},
	{"Tab / Shift+Tab", "循环切换 Tab"},
	{"S", "全局搜索"},
	{"?", "键位总览"},
	{"q / ctrl+c", "退出"},
}

// helpTab is the keybinding overview overlay (triggered by `?`).
//
// It renders in place of the tab content, like the search overlay: while open
// it captures every key, and `?`/`esc` close it.
type helpTab struct {
	title    string
	bindings []KeyBinding
	width    int
	height   int
}

// helpCloseMsg asks the top-level model to close the help overlay.
type helpCloseMsg struct{}

// newHelpTab builds the overlay for a tab. Tab-specific rows are parsed out of
// the tab's own Help() string so the overlay and the status bar cannot drift.
func newHelpTab(title, activeHelp string) *helpTab {
	return &helpTab{title: title, bindings: parseHelp(activeHelp)}
}

// parseHelp splits "keys desc · keys desc" into rows. The first space separates
// the key column from the description, matching every tab's Help() format.
func parseHelp(help string) []KeyBinding {
	var out []KeyBinding
	for _, part := range strings.Split(help, " · ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		keys, desc, found := strings.Cut(part, " ")
		if !found {
			out = append(out, KeyBinding{Keys: part})
			continue
		}
		out = append(out, KeyBinding{Keys: keys, Desc: strings.TrimSpace(desc)})
	}
	return out
}

func (h *helpTab) Title() string     { return "Help" }
func (h *helpTab) Help() string      { return "? / esc close" }
func (h *helpTab) InputMode() bool   { return true }
func (h *helpTab) Init() tea.Cmd     { return nil }
func (h *helpTab) SetSize(w, hh int) { h.width, h.height = w, hh }

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
	return clipLines(box, h.height)
}

// renderBinding renders one key/desc row with a fixed key column so the
// descriptions line up.
func renderBinding(b KeyBinding) string {
	return fmt.Sprintf("  %-18s %s", b.Keys, b.Desc)
}

// Compile-time guard.
var _ Tab = (*helpTab)(nil)
