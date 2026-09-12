package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SyncState 是 TUI 底部常驻同步状态条的快照。Err 非空表示最近一次同步
// 失败；Last 是最近一次成功 pull/push 的时间（零值表示未知）；LastPull
// 只反映最近一次成功 pull（audit 面板"自上次 pull"过滤视图用，零值表示
// 从未 pull 过）。
type SyncState struct {
	Dirty    int
	Last     time.Time
	LastPull time.Time
	Err      error
}

// PullOutcome 汇总一次后台拉取。零值表示没有网络动作（节流/锁忙跳过）或
// 远端无变更。Err 含 client 被屏蔽的情况，调用方可用 errors.Is 判别。
type PullOutcome struct {
	Applied         int
	MetadataUpdated bool
	Err             error
}

// SyncSource 提供自动同步状态、写后推送与后台拉取。server 模式且开启
// auto_sync 时由 cmd 层注入；git 模式或关闭 auto_sync 时为 nil，TUI 不显示
// 同步状态，也不触发任何 push/pull（见 tui-viewer 的同步状态可见性要求）。
type SyncSource interface {
	// Status 只读本地状态（待推送条数 / 最近同步时间），不得发起网络请求。
	Status() SyncState
	// Push 在内部预算内做一次 best-effort 推送，返回推送后的状态。
	Push() SyncState
	// Pull 在内部预算内做一次 best-effort 拉取；refresh=true 绕过节流窗口。
	// 不打印、不退出进程：结果只反映在返回的 outcome 里。
	Pull(refresh bool) PullOutcome
}

// syncPullMsg 携带一次后台拉取的结果。
type syncPullMsg struct{ out PullOutcome }

// pullSync runs one best-effort background pull (the budget lives in the
// injected SyncSource). It is issued at startup so the TUI renders local data
// first and converges on the server state once the pull lands.
func pullSync(src SyncSource, refresh bool) tea.Cmd {
	if src == nil {
		return nil
	}
	return func() tea.Msg { return syncPullMsg{out: src.Pull(refresh)} }
}

// syncStatusMsg 携带一次 Status/Push 的结果。
type syncStatusMsg struct{ state SyncState }

// refreshSync queries the local sync status without touching the network.
func (m Model) refreshSync() tea.Cmd {
	if m.sync == nil {
		return nil
	}
	src := m.sync
	return func() tea.Msg { return syncStatusMsg{state: src.Status()} }
}

// pushSync runs one best-effort background push (the budget lives in the
// injected SyncSource) and reports the resulting status back.
func (m Model) pushSync() tea.Cmd {
	if m.sync == nil {
		return nil
	}
	src := m.sync
	return func() tea.Msg { return syncStatusMsg{state: src.Push()} }
}

// reloadAllTabs asks every tab to drop its cached data and reload, so the UI
// converges on the working copy after a background pull applies changes. Tabs
// are held by pointer, so the reloads land on the live tab instances.
func reloadAllTabs(m Model) tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.tabs {
		if c := t.Reload(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// syncBadge renders the persistent sync status for the bottom bar. An empty
// string means automatic sync is unavailable (git mode / auto_sync off), so the
// bottom bar keeps its plain help layout.
func (m Model) syncBadge() string {
	if m.sync == nil {
		return ""
	}
	switch {
	case m.syncState.Err != nil:
		return fmt.Sprintf("⟳ %d pending push · sync failed: %s", m.syncState.Dirty, shortReason(m.syncState.Err))
	case m.syncState.Dirty > 0:
		return fmt.Sprintf("⟳ %d pending push", m.syncState.Dirty)
	case !m.syncState.Last.IsZero():
		return "⟳ synced " + m.syncState.Last.Local().Format("15:04")
	default:
		return "⟳ synced"
	}
}

// shortReason condenses a sync error into one short, single-line phrase.
func shortReason(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if i := strings.IndexAny(msg, "\n\r"); i >= 0 {
		msg = msg[:i]
	}
	return truncateWidth(msg, 24)
}

// writeDoneMsg reports whether a message signals a completed write operation
// (as opposed to a plain read/reload of the same tab).
func writeDoneMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case envReloadMsg, textReloadMsg, configReloadMsg, configCreatedMsg, sshReloadMsg, aiProviderReloadMsg, mcpReloadMsg:
		return true
	}
	return false
}

// bottomBar composes the bottom line: transient status text on the left, the
// persistent sync badge right-aligned when automatic sync is available.
func (m Model) bottomBar(left string, style lipgloss.Style) string {
	badge := m.syncBadge()
	available := m.width - 2 - 2 // frame border + statusBarStyle padding
	if available < 1 {
		return style.Render(truncateWidth(left, 1))
	}
	if badge == "" {
		return style.Render(truncateWidth(left, available))
	}
	badgeText := truncateWidth(badge, available/2)
	badgeW := lipgloss.Width(badgeText)
	text := truncateWidth(left, available-badgeW-1)
	gap := available - lipgloss.Width(text) - badgeW
	if gap < 1 {
		gap = 1
	}
	return style.Render(text + strings.Repeat(" ", gap) + syncBadgeStyle.Render(badgeText))
}
