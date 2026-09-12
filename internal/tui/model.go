package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/text"
)

// Managers bundles the three domain managers shared by all tabs. The TUI is a
// pure interaction layer over these existing managers (no storage changes).
// History 为 nil 时不注册 History Tab（git 模式 / 未配置 server）；
// Audit 为 nil 时不注册审计 Tab；LLM 为 nil 时不注册 AI Tab；
// MCP 为 nil 时不注册 MCP Tab。
type Managers struct {
	Env    *env.Manager
	Text   *text.Manager
	Config *config.Manager
	SSH    *ssh.Manager
	LLM    *llm.ProviderManager
	MCP    *mcp.Manager
	// MCPLedger 是本机导出台账路径；空时按用户默认位置解析。
	MCPLedger string
	// MCPHome 是 Coding Agent 配置根目录（通常为用户 HOME）；空时导出器
	// 回退到 os.UserHomeDir。测试注入临时目录以免写进真实 agent 配置。
	MCPHome string
	// LLMPointer/LLMHome 是 agent 指针文件路径与 agent 配置根目录；
	// 为空时按用户默认位置解析（测试可注入临时目录）。
	LLMPointer string
	LLMHome    string
	// LLMCatalog 是 models.dev 模型目录缓存路径；为空时按目录 provider 装配
	// 模型集会报「缓存缺失」。
	LLMCatalog string
	History    HistorySource
	Audit      AuditSource
	// AuditWriter 记录 TUI 内写操作的业务审计事件；nil 时不记录（只读嵌入或测试）。
	AuditWriter AuditWriter
	// Sync 提供自动同步状态与写后推送；nil（git 模式 / 未开 auto_sync）时
	// 底部不显示同步状态，写操作也不触发 push。
	Sync SyncSource
	// Refresh 透传 `senv tui --refresh`：启动后台拉取绕过节流窗口。TUI 从不
	// 因网络阻塞——本地数据先行渲染，拉取完成后自动更新界面。
	Refresh bool
	// snap 是 env vault 的进程内共享快照。由 New 注入；写操作与 pull 应用
	// 变更后失效。nil 时消费方直接走 Manager.Snapshot。
	snap *snapshotRegistry
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
	warn   string
	toast  string
	level  toastLevel
	search *searchTab // non-nil while the global search overlay is open
	help   *helpTab   // non-nil while the keybinding overview overlay is open
	// sync 是自动同步数据源（可为 nil）；syncState 是该源的最近一次快照。
	sync      SyncSource
	syncState SyncState
	// quitArmed 记录「仍有待推送」提示已经显示过一次，第二次 q 才退出。
	quitArmed bool
	// toastSeq identifies the newest toast so an older expiry timer cannot
	// clear a message that arrived after it.
	toastSeq int
}

// toastLevel distinguishes a transient success hint from a transient warning.
type toastLevel int

const (
	toastSuccess toastLevel = iota
	toastWarn
)

// toastTTL is how long a transient success/warning hint stays on screen.
const toastTTL = 3 * time.Second

// toastMsg shows a transient message in the bottom bar.
type toastMsg struct {
	text  string
	level toastLevel
}

// clearToastMsg expires one specific toast (matched by sequence).
type clearToastMsg struct{ seq int }

// okToast returns a command showing a transient success hint.
func okToast(text string) tea.Cmd {
	return func() tea.Msg { return toastMsg{text: text, level: toastSuccess} }
}

// warnToast returns a command showing a transient warning hint.
func warnToast(text string) tea.Cmd {
	return func() tea.Msg { return toastMsg{text: text, level: toastWarn} }
}

// New creates the TUI model backed by the given managers. SSH is registered
// only when supplied; this keeps existing tests and limited integrations stable.
func New(mgr Managers) Model {
	if mgr.snap == nil {
		mgr.snap = newSnapshotRegistry(mgr.Env)
	}
	m := Model{mgr: mgr, sync: mgr.Sync}
	m.tabs = []Tab{
		newEnvTab(mgr),
		newTextTab(mgr),
		newConfigTab(mgr),
	}
	if mgr.SSH != nil {
		m.tabs = append(m.tabs, newSSHTab(mgr))
	}
	if mgr.LLM != nil {
		m.tabs = append(m.tabs, newAITab(mgr))
	}
	if mgr.MCP != nil {
		m.tabs = append(m.tabs, newMCPTab(mgr))
	}
	if mgr.History != nil {
		m.tabs = append(m.tabs, newHistoryTab(mgr.History))
	}
	if mgr.Audit != nil {
		m.tabs = append(m.tabs, newAuditTab(mgr.Audit, mgr.Sync))
	}
	return m
}

// Init performs initial setup. Tabs load their data lazily on first focus,
// and the server pull (when automatic sync is available) runs in the
// background: local cached data renders immediately and the tabs reload once
// the pull applies remote changes.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.tabs {
		if c := t.Init(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if c := m.refreshSync(); c != nil {
		cmds = append(cmds, c)
	}
	if c := pullSync(m.sync, m.mgr.Refresh); c != nil {
		cmds = append(cmds, c)
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

// warnMsg carries a non-fatal warning to be rendered in the warning bar.
type warnMsg struct{ text string }

// clearWarnMsg clears the warning bar.
type clearWarnMsg struct{}

// renameDoneMsg reports a successful rename and asks the owning tab to reload
// with the cursor parked on the new name.
type renameDoneMsg struct {
	group string
	key   string
	text  string
}

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
	case helpCloseMsg:
		m.help = nil
		return m, nil
	}

	// While the search overlay is open, route all other messages to it.
	if m.search != nil {
		next, cmd := m.search.Update(msg)
		m.search = next.(*searchTab)
		return m, cmd
	}
	// Same for the help overlay: it owns every key while it is open.
	if m.help != nil {
		next, cmd := m.help.Update(msg)
		m.help = next.(*helpTab)
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

	case warnMsg:
		m.warn = msg.text
		return m, nil

	case toastMsg:
		m.toast = msg.text
		m.level = msg.level
		m.toastSeq++
		seq := m.toastSeq
		return m, tea.Tick(toastTTL, func(time.Time) tea.Msg { return clearToastMsg{seq} })

	case clearToastMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
		return m, nil

	case clearWarnMsg:
		m.warn = ""
		return m, nil

	case syncStatusMsg:
		m.syncState = msg.state
		return m, nil

	case syncPullMsg:
		// 后台拉取结束：错误进错误栏（含被屏蔽提示，不退出进程）；应用了
		// 变更则提示并让所有 Tab 重载本地（已更新的）工作副本；无变更或
		// 零网络跳过时只更新同步徽标。
		if msg.out.Err != nil {
			m.err = msg.out.Err.Error()
			return m, m.refreshSync()
		}
		if msg.out.Applied > 0 || msg.out.MetadataUpdated {
			m.mgr.snap.Invalidate()
			return m, tea.Batch(
				okToast(fmt.Sprintf("updated %d entries from server", msg.out.Applied)),
				reloadAllTabs(m),
				m.refreshSync(),
			)
		}
		return m, m.refreshSync()

	case tea.KeyMsg:
		// If the active tab is capturing text input, forward ALL keys so global
		// shortcuts do not hijack typing.
		if m.tabs[m.active].InputMode() {
			var cmd tea.Cmd
			m.tabs[m.active], cmd = m.tabs[m.active].Update(msg)
			return m, cmd
		}

		// Any keypress clears a stale error/warning banner (task 11.1), except quit.
		hadErr := m.err != ""
		hadWarn := m.warn != ""
		hadToast := m.toast != ""

		// Number keys jump straight to the Nth registered tab (1–9). Out-of-range
		// digits are ignored so limited integrations (e.g. git mode with 3 tabs)
		// stay usable.
		if idx, ok := tabIndexFor(msg.String(), len(m.tabs)); ok {
			m.err = ""
			m.warn = ""
			m.active = idx
			return m, m.activateTab(idx)
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// Warn once when local changes are still unpushed, so quitting a
			// long-running TUI does not silently drop the sync attempt.
			if m.sync != nil && m.syncState.Dirty > 0 && !m.quitArmed {
				m.quitArmed = true
				m.warn = fmt.Sprintf("%d entries still pending push, press q again to quit (or wait for auto sync)", m.syncState.Dirty)
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+r":
			// 刷新当前 Tab（grill D7：refresh 统一 Ctrl+R，腾出 r=rename）。
			m.err = ""
			return m, m.tabs[m.active].Reload()
		case "S":
			// Open the global cross-type search overlay (task 10.1).
			m.search = newSearchTab(m.mgr)
			m.search.SetSize(m.width, m.height)
			return m, m.search.Init()
		case "?":
			// Open the keybinding overview for the active tab.
			m.help = newHelpTab(m.tabs[m.active].Title(), m.tabs[m.active])
			m.help.SetSize(m.width, m.height)
			return m, nil
		case "tab":
			m.err = ""
			m.warn = ""
			m.active = (m.active + 1) % len(m.tabs)
			return m, m.activateTab(m.active)
		case "shift+tab":
			m.err = ""
			m.warn = ""
			m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
			return m, m.activateTab(m.active)
		}

		// Swallow the key that dismissed the banner so the user sees it clear
		// before the next action takes effect.
		if hadErr {
			m.err = ""
			return m, nil
		}
		if hadWarn {
			m.warn = ""
			return m, nil
		}
		// A toast is informational: clear it but still perform the action.
		if hadToast {
			m.toast = ""
		}
	}

	// Default: forward to the active tab. A completed load is broadcast to
	// every tab so a background tab's data lands even when the user has
	// switched away (otherwise the load is dropped and re-issued on return).
	// A completed write additionally refreshes the sync badge and kicks off a
	// best-effort background push.
	if isLoadBroadcastMsg(msg) {
		var cmds []tea.Cmd
		for i, tab := range m.tabs {
			var c tea.Cmd
			m.tabs[i], c = tab.Update(msg)
			cmds = append(cmds, c)
		}
		return m, tea.Batch(cmds...)
	}
	var cmd tea.Cmd
	m.tabs[m.active], cmd = m.tabs[m.active].Update(msg)
	if writeDoneMsg(msg) {
		m.mgr.snap.Invalidate()
		if c := m.refreshSync(); c != nil {
			cmd = tea.Batch(cmd, c)
		}
		if c := m.pushSync(); c != nil {
			cmd = tea.Batch(cmd, c)
		}
	}
	return m, cmd
}

// isLoadBroadcastMsg 报告一条消息是否为 Tab 数据装载完成事件；这类事件属于
// 发出请求的那个 Tab，但后台 Tab 装载完成时用户可能已切走，需要广播投递。
func isLoadBroadcastMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case envLoadedMsg, textLoadedMsg, configLoadedMsg, sshLoadedMsg,
		aiLoadedMsg, mcpLoadedMsg, auditLoadedMsg, historyLoadedMsg:
		return true
	}
	return false
}

// tabIndexFor maps a single digit key ("1"–"9") to a zero-based tab index.
// It reports false for non-digit keys and for digits past the last tab.
func tabIndexFor(key string, tabs int) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	idx := int(key[0] - '1')
	if idx >= tabs {
		return 0, false
	}
	return idx, true
}

// activateTab 把 Tab 标记为已激活（History Tab 依赖它做首次激活延迟加载）
// 并执行该 Tab 的 Init。
func (m Model) activateTab(idx int) tea.Cmd {
	if h, ok := m.tabs[idx].(*historyTab); ok {
		h.visited = true
	}
	return m.tabs[idx].Init()
}

// applyJump closes the overlay and moves the cursor to the chosen entry across
// tab + group + item (task 10.3).
func (m Model) applyJump(j searchJumpMsg) (tea.Model, tea.Cmd) {
	m.search = nil
	m.err = ""
	// Resolve the target tab by title so optional tabs (SSH / AI) work without
	// hard-coded indices.
	want := tabTitleForResult(j.resultType)
	for i, t := range m.tabs {
		if t.Title() != want {
			continue
		}
		m.active = i
		if f, ok := t.(jumpFocuser); ok {
			f.focusJump(j.group, j.key)
		}
		return m, m.activateTab(i)
	}
	return m, nil
}

// jumpFocuser is implemented by tabs that can position their cursor on an entry
// (used by the global search overlay).
type jumpFocuser interface{ focusJump(group, key string) }

// tabTitleForResult maps a search result type to its tab title.
func tabTitleForResult(resultType string) string {
	if resultType == typeConfig {
		return "Config"
	}
	return resultType
}

// View renders the tab strip + active tab content + status/error bar.
func (m Model) View() string {
	if m.height == 0 {
		return "starting…"
	}

	// Minimum size guard: the outer frame (2 rows) + tab strip with its
	// underline (2 rows) + status bar (1 row) + tab pane borders (2 rows)
	// need height >= 7, and the shortest tab strip needs width >= 30. Below
	// this the layout collapses, so show a plain centered hint with no chrome.
	if m.height < 7 || m.width < 30 {
		hint := "terminal too small (need ≥30×7 chars)"
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
	// tabStripStyle 的 Padding(0,1) 占 2 列：预算内放不下的尾部 tab 以 "…"
	// 收尾，否则宽标签会在 strip 内折行、把整个布局撑高。
	strip := fitTabStrip(tabParts, m.tabs[m.active].Title(), m.active, contentW-2)
	tabStrip := tabStripStyle.Width(contentW).Render(strip)

	// Active tab content. Overlays take over the content area while open.
	var content string
	switch {
	case m.search != nil:
		content = m.search.View()
	case m.help != nil:
		content = m.help.View()
	default:
		content = m.tabs[m.active].View()
	}

	// Bottom bar: error takes precedence over warning, then status hint. The
	// text is hard-truncated (not Width-wrapped, which would add a line) to
	// fit inside contentW minus the bar's Padding(0,1).
	var bottom string
	switch {
	case m.err != "":
		bottom = m.bottomBar("⚠ "+m.err, errorBarStyle)
	case m.warn != "":
		bottom = m.bottomBar("⚠ "+m.warn, warnBarStyle)
	case m.toast != "":
		style := statusBarStyle.Foreground(lipgloss.Color(colorSuccess))
		prefix := "✓ "
		if m.level == toastWarn {
			style = warnBarStyle
			prefix = "⚠ "
		}
		bottom = m.bottomBar(prefix+m.toast, style)
	default:
		bottom = m.bottomBar(groupBar(m.tabs[m.active].Bindings()), statusBarStyle)
	}

	// Stack the chrome inside the frame. lipgloss v1.x draws borders
	// outside Width/Height, so Width(m.width-2).Height(m.height-2) makes
	// the frame's total rendered size exactly m.width x m.height.
	inner := lipgloss.JoinVertical(lipgloss.Left, tabStrip, content, bottom)
	return frameStyle.Width(m.width - 2).Height(m.height - 2).Render(inner)
}

// fitTabStrip 把 tab 条部件截进 budget 显示列：从左往右放，放不下的尾部
// 以 muted "…" 示意（数字键/Tab 仍可到达被收起的 tab）。若连激活 tab 都放
// 不下（接近最小宽度），退化为只渲染激活 tab（标签截断到预算内）。
func fitTabStrip(parts []string, activeTitle string, active, budget int) string {
	if budget < 4 {
		budget = 4
	}
	var out []string
	used := 0
	cut := -1
	for i, p := range parts {
		w := lipgloss.Width(p)
		if used+w > budget {
			cut = i
			break
		}
		out = append(out, p)
		used += w
	}
	if cut < 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, out...)
	}
	// 激活 tab（parts 下标 2*active）被截掉时，退化为只渲染激活 tab。
	if 2*active >= cut {
		return activeTabStyle.Render(truncateWidth(activeTitle, maxInt(budget-2, 1)))
	}
	if used+1 <= budget {
		out = append(out, mutedStyle().Render("…"))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, out...)
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
