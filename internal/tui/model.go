package tui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/text"
)

// Managers bundles the three domain managers shared by all tabs. The TUI is a
// pure interaction layer over these existing managers (no storage changes).
type Managers struct {
	Env    *env.Manager
	Text   *text.Manager
	Config *config.Manager
}

// Model is the top-level bubbletea model. It owns the tab strip, the currently
// active tab, the content area size, and the transient error banner state.
//
// Each tab is held by pointer, so its navigation state (selected group, item
// cursor, filter, ...) is preserved across tab switches — switching away and
// back restores the previous selection.
type Model struct {
	mgr    Managers
	tabs   []Tab
	active int
	width  int
	height int
	err    string
	search *searchTab // non-nil while the global search overlay is open
}

// New creates the TUI model backed by the given managers.
func New(mgr Managers) Model {
	m := Model{mgr: mgr}
	m.tabs = []Tab{
		newEnvTab(mgr),
		newTextTab(mgr),
		newConfigTab(mgr),
	}
	return m
}

// Init performs initial setup. Tabs load their data lazily on first focus.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.tabs {
		if c := t.Init(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// errMsg carries an error from a tab/manager to be rendered in the error bar.
type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

// clearErrMsg clears the error bar.
type clearErrMsg struct{}

// clearError returns a command that clears the error bar.
func clearError() tea.Cmd { return func() tea.Msg { return clearErrMsg{} } }

// Update routes messages. Sizing, tab switching, search overlay and error
// handling live here; tab-specific keys are forwarded to the active tab.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Search overlay navigation messages are handled at the top level.
	switch msg := msg.(type) {
	case searchJumpMsg:
		return m.applyJump(msg)
	case searchCloseMsg:
		m.search = nil
		return m, nil
	}

	// While the search overlay is open, route all other messages to it.
	if m.search != nil {
		next, cmd := m.search.Update(msg)
		m.search = next.(*searchTab)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Inner content area. lipgloss v1.x draws borders OUTSIDE Width/Height,
		// so each pane renders (Width+2)x(Height+2). The height budget covers:
		//   frame top+bottom border : 2 rows
		//   tab strip (labels+underline): 2 rows
		//   status/error bar        : 1 row
		//   tab's own pane borders  : 2 rows  (panes render Height+2)
		// Total chrome = 7 rows, so contentH = height - 7.
		// Width: the frame border consumes 2 cols, so contentW = width - 2.
		contentW := m.width - 2
		contentH := m.height - 7
		if contentH < 0 {
			contentH = 0
		}
		if contentW < 1 {
			contentW = 1
		}
		for _, t := range m.tabs {
			t.SetSize(contentW, contentH)
		}
		return m, nil

	case errMsg:
		m.err = msg.err.Error()
		return m, nil

	case clearErrMsg:
		m.err = ""
		return m, nil

	case tea.KeyMsg:
		// If the active tab is capturing text input, forward ALL keys so global
		// shortcuts do not hijack typing.
		if m.tabs[m.active].InputMode() {
			var cmd tea.Cmd
			m.tabs[m.active], cmd = m.tabs[m.active].Update(msg)
			return m, cmd
		}

		// Any keypress clears a stale error banner (task 11.1), except quit.
		hadErr := m.err != ""
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "S":
			// Open the global cross-type search overlay (task 10.1).
			m.search = newSearchTab(m.mgr)
			m.search.SetSize(m.width, m.height)
			return m, m.search.Init()
		case "1":
			m.err = ""
			m.active = 0
			return m, m.tabs[0].Init()
		case "2":
			m.err = ""
			m.active = 1
			return m, m.tabs[1].Init()
		case "3":
			m.err = ""
			m.active = 2
			return m, m.tabs[2].Init()
		case "tab":
			m.err = ""
			m.active = (m.active + 1) % len(m.tabs)
			return m, m.tabs[m.active].Init()
		case "shift+tab":
			m.err = ""
			m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
			return m, m.tabs[m.active].Init()
		}

		// Swallow the key that dismissed the banner so the user sees it clear
		// before the next action takes effect.
		if hadErr {
			m.err = ""
			return m, nil
		}
	}

	// Default: forward to the active tab.
	var cmd tea.Cmd
	m.tabs[m.active], cmd = m.tabs[m.active].Update(msg)
	return m, cmd
}

// applyJump closes the overlay and moves the cursor to the chosen entry across
// tab + group + item (task 10.3).
func (m Model) applyJump(j searchJumpMsg) (tea.Model, tea.Cmd) {
	m.search = nil
	m.err = ""
	switch j.resultType {
	case typeEnv:
		m.active = 0
		if et, ok := m.tabs[0].(*envTab); ok {
			et.focusJump(j.group, j.key)
		}
	case typeText:
		m.active = 1
		if tt, ok := m.tabs[1].(*textTab); ok {
			tt.focusJump(j.group, j.key)
		}
	case typeConfig:
		m.active = 2
		if ct, ok := m.tabs[2].(*configTab); ok {
			ct.focusJump(j.key)
		}
	}
	return m, nil
}

// View renders the tab strip + active tab content + status/error bar.
func (m Model) View() string {
	if m.height == 0 {
		return "starting..."
	}

	// Minimum size guard: the outer frame (2 rows) + tab strip with its
	// underline (2 rows) + status bar (1 row) + tab pane borders (2 rows)
	// need height >= 7, and the shortest tab strip needs width >= 30. Below
	// this the layout collapses, so show a plain centered hint with no chrome.
	if m.height < 7 || m.width < 30 {
		hint := "terminal too small (need ≥30×7)"
		// Center the hint within the available area without any box drawing.
		padLines := (m.height - 1) / 2
		if padLines < 0 {
			padLines = 0
		}
		top := ""
		for i := 0; i < padLines; i++ {
			top += "\n"
		}
		pad := (m.width - len([]rune(hint))) / 2
		if pad < 0 {
			pad = 0
		}
		return top + strings.Repeat(" ", pad) + hint
	}

	// Inner content dimensions (frame border + chrome rows already reserved
	// in Update()). The tab strip and bottom bar share contentW.
	contentW := m.width - 2

	// Tab strip: active tab gets the accent background block, inactive tabs
	// get muted text, and a `│` separator is rendered between adjacent tabs.
	// Two separators for three tabs.
	separator := tabSeparatorStyle.Render("│")
	tabParts := make([]string, 0, len(m.tabs)*2-1)
	for i, t := range m.tabs {
		if i > 0 {
			tabParts = append(tabParts, separator)
		}
		label := t.Title()
		if i == m.active {
			tabParts = append(tabParts, activeTabStyle.Render(label))
		} else {
			tabParts = append(tabParts, tabStyle.Render(label))
		}
	}
	tabStrip := tabStripStyle.Width(contentW).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, tabParts...),
	)

	// Active tab content.
	content := m.tabs[m.active].View()

	// Bottom bar: error takes precedence over the status hint. The text is
	// hard-truncated (not Width-wrapped, which would add a line) to fit
	// inside contentW minus the bar's Padding(0,1).
	var bottom string
	if m.err != "" {
		bottom = errorBarStyle.Render("⚠ " + truncateRunes(m.err, contentW-4))
	} else {
		bottom = statusBarStyle.Render(truncateRunes(m.tabs[m.active].Help(), contentW-2))
	}

	// Stack the chrome inside the frame. lipgloss v1.x draws borders
	// outside Width/Height, so Width(m.width-2).Height(m.height-2) makes
	// the frame's total rendered size exactly m.width x m.height.
	inner := lipgloss.JoinVertical(lipgloss.Left, tabStrip, content, bottom)
	return frameStyle.Width(m.width - 2).Height(m.height - 2).Render(inner)
}

// truncateRunes truncates s to at most max runes, appending "…" if shortened.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
