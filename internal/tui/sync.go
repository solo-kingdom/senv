package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SyncState 是 TUI 底部常驻同步状态条的快照。Err 非空表示最近一次同步
// 失败；Last 是最近一次成功 pull/push 的时间（零值表示未知）。
type SyncState struct {
	Dirty int
	Last  time.Time
	Err   error
}

// SyncSource 提供自动同步状态与写后推送。server 模式且开启 auto_sync 时由
// cmd 层注入；git 模式或关闭 auto_sync 时为 nil，TUI 不显示同步状态，也不
// 触发任何 push（见 tui-viewer 的同步状态可见性要求）。
type SyncSource interface {
	// Status 只读本地状态（待推送条数 / 最近同步时间），不得发起网络请求。
	Status() SyncState
	// Push 在内部预算内做一次 best-effort 推送，返回推送后的状态。
	Push() SyncState
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

// syncBadge renders the persistent sync status for the bottom bar. An empty
// string means automatic sync is unavailable (git mode / auto_sync off), so the
// bottom bar keeps its plain help layout.
func (m Model) syncBadge() string {
	if m.sync == nil {
		return ""
	}
	switch {
	case m.syncState.Err != nil:
		return fmt.Sprintf("⟳ %d 条待推送 · 同步失败：%s", m.syncState.Dirty, shortReason(m.syncState.Err))
	case m.syncState.Dirty > 0:
		return fmt.Sprintf("⟳ %d 条待推送", m.syncState.Dirty)
	case !m.syncState.Last.IsZero():
		return "⟳ 已同步 " + m.syncState.Last.Local().Format("15:04")
	default:
		return "⟳ 已同步"
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
	case envReloadMsg, textReloadMsg, configReloadMsg, configCreatedMsg, sshReloadMsg, aiProviderReloadMsg:
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
