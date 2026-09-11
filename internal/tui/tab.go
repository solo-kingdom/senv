package tui

import tea "github.com/charmbracelet/bubbletea"

// Tab is the interface implemented by each tab view (Env / Text / Config).
//
// Each tab owns its own navigation state (selected group, selected item,
// filter, etc.) so that switching tabs and back preserves the cursor.
type Tab interface {
	// Title returns the label shown in the tab strip.
	Title() string

	// Bindings 声明当前模式下的键位（keymap 注册表）。它是键位唯一真相
	// 源：Update 分发与状态栏提示、`?` 键位总览渲染共用同一组常量，键位
	// 与帮助不可能漂移。同一动作在不同 Tab 使用相同按键。
	Bindings() []KeyAction

	// InputMode reports whether the tab is currently capturing text input
	// (e.g. an inline edit modal or filter box). When true, the top-level
	// model forwards ALL key messages to the tab instead of intercepting
	// global shortcuts (q, 1/2/3, Tab).
	InputMode() bool

	// Init performs initial command setup for the tab. Tabs load lazily on
	// first focus; subsequent calls are no-ops.
	Init() tea.Cmd

	// Reload drops cached data and reloads from the managers. The top level
	// calls it after a background sync applies remote changes.
	Reload() tea.Cmd

	// Update handles a message and returns the (possibly mutated) tab.
	Update(msg tea.Msg) (Tab, tea.Cmd)

	// View renders the tab content (without the tab strip / status bar).
	View() string

	// SetSize informs the tab of the available content area size.
	SetSize(width, height int)
}
