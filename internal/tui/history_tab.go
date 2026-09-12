package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/provider"
)

// HistorySource 是 History Tab 的数据源（server 模式由 cmd 层注入；nil 时
// 不注册该 Tab）。查询/恢复走 provider 既有路径，解密由注入方完成。
type HistorySource interface {
	History(ctx context.Context, f provider.HistoryFilter) ([]provider.HistoryVersion, error)
	DecryptHistory(v provider.HistoryVersion) (string, error)
	Restore(ctx context.Context, v provider.HistoryVersion) error
}

// historyTab 渲染条目历史：recent（vault 最近变更）→ entry（单条目版本
// 时间线）→ detail（选中版本解密内容）→ confirm（恢复确认）。
type historyTab struct {
	source        HistorySource
	width, height int
	loaded        bool
	// visited 延迟加载开关：启动批量 Init 不触发查询（不发网络请求），
	// 用户首次激活本 Tab 时由顶层置位后才装载。
	visited bool

	rows []provider.HistoryVersion
	// list 承载游标与窗口（共享列表组件），替代手写 visibleRows 居中窗口；
	// 窗口语义由「光标居中」统一为「跟随光标」，可见内容集合不变。
	list    List
	entryID string // entry 模式的条目标识（kind:grp:key），空 = recent 模式
	flash   string
	mode    historyMode
}

type historyMode int

const (
	historyModeRecent historyMode = iota
	historyModeEntry
	historyModeDetail
	historyModeConfirm
)

type historyLoadedMsg struct {
	rows []provider.HistoryVersion
	id   string
	err  error
}

type historyRestoredMsg struct{ err error }

func newHistoryTab(source HistorySource) *historyTab {
	return &historyTab{source: source}
}

func (t *historyTab) Title() string { return "History" }

func (t *historyTab) Bindings() []KeyAction {
	switch t.mode {
	case historyModeEntry:
		return []KeyAction{actUp, actDown, actTop, actBottom, actPageUp, actPageDn,
			{[]string{"enter"}, "view revisions", grpItem}, {[]string{"R"}, "restore", grpItem}, actEsc}
	case historyModeDetail:
		return []KeyAction{{[]string{"R"}, "restore", grpItem}, actEsc}
	case historyModeConfirm:
		return []KeyAction{{[]string{"enter/y"}, "confirm restore", grpConfirm}, {[]string{"esc/n"}, "cancel", grpConfirm}}
	default:
		return []KeyAction{actUp, actDown, actTop, actBottom, actPageUp, actPageDn,
			{[]string{"enter"}, "entry history", grpItem}, {[]string{"R"}, "restore", grpItem}}
	}
}

func (t *historyTab) InputMode() bool { return t.mode == historyModeConfirm }

func (t *historyTab) Init() tea.Cmd {
	if !t.visited || t.loaded {
		return nil
	}
	return t.load("")
}

// Reload re-queries history for the currently visible entry (or the recent
// list); the top level calls it after a background sync applies remote changes.
// 从未激活过时保持延迟语义：只失效缓存，不发查询。
func (t *historyTab) Reload() tea.Cmd {
	t.loaded = false
	if !t.visited {
		return nil
	}
	return t.load(t.entryID)
}

func (t *historyTab) load(entryID string) tea.Cmd {
	source := t.source
	return func() tea.Msg {
		f := provider.HistoryFilter{Limit: 50}
		if entryID != "" {
			kind, grp, key, ok := splitEntryID(entryID)
			if !ok {
				return historyLoadedMsg{err: fmt.Errorf("invalid entry id %q", entryID)}
			}
			f = provider.HistoryFilter{Kind: kind, Grp: grp, Key: key, Limit: 50}
		}
		rows, err := source.History(context.Background(), f)
		if err != nil {
			return historyLoadedMsg{err: err}
		}
		return historyLoadedMsg{rows: rows, id: entryID}
	}
}

func splitEntryID(s string) (kind, grp, key string, ok bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func entryIDOf(h provider.HistoryVersion) string {
	return h.Kind + ":" + h.Grp + ":" + h.Key
}

func (t *historyTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case historyLoadedMsg:
		t.loaded = true
		if msg.err != nil {
			return t, func() tea.Msg { return errMsg{err: msg.err} }
		}
		t.rows = msg.rows
		t.entryID = msg.id
		t.list.Home()
		if t.mode == historyModeConfirm || t.mode == historyModeDetail {
			t.mode = historyModeEntry
		}
		if t.entryID == "" {
			t.mode = historyModeRecent
		} else {
			t.mode = historyModeEntry
		}
		return t, nil

	case historyRestoredMsg:
		if msg.err != nil {
			return t, func() tea.Msg { return errMsg{err: msg.err} }
		}
		t.flash = "✓ restored and pushed (new revision created)"
		t.mode = historyModeEntry
		t.loaded = true
		return t, t.load(t.entryID)
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				t.list.Move(-1, len(t.rows))
				return t, nil
			}
		case "down", "j":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				t.list.Move(1, len(t.rows))
				return t, nil
			}
		case "enter":
			switch t.mode {
			case historyModeConfirm:
				row := t.current()
				if row == nil {
					return t, nil
				}
				return t, t.doRestore(*row)
			case historyModeDetail:
				t.mode = historyModeEntry
				return t, nil
			case historyModeEntry:
				if row := t.current(); row != nil {
					t.mode = historyModeDetail
					return t, nil
				}
			case historyModeRecent:
				if row := t.current(); row != nil && !row.Deleted {
					id := entryIDOf(*row)
					t.mode = historyModeEntry
					return t, t.load(id)
				}
			}
		case "R":
			// R=恢复（r 已统一为重命名语义；grill D7）
			if t.mode == historyModeRecent || t.mode == historyModeEntry || t.mode == historyModeDetail {
				if row := t.current(); row != nil && !row.Deleted {
					t.mode = historyModeConfirm
					return t, nil
				}
			}
		case "g":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				t.list.Home()
			}
		case "G":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				t.list.End(len(t.rows))
			}
		case "pgup":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				t.list.Page(-1, len(t.rows))
			}
		case "pgdown":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				t.list.Page(1, len(t.rows))
			}
		case "esc":
			switch t.mode {
			case historyModeConfirm:
				t.mode = historyModeEntry
				return t, nil
			case historyModeDetail:
				t.mode = historyModeEntry
				return t, nil
			case historyModeEntry:
				t.mode = historyModeRecent
				t.entryID = ""
				return t, t.load("")
			default:
				return t, nil
			}
		case "n":
			if t.mode == historyModeConfirm {
				t.mode = historyModeEntry
				return t, nil
			}
		}
		// q 不在本 Tab 处理：非确认模式由顶层 dirty-quit 守卫统一接管
		//（有待推送先提示再退）；确认模式下 InputMode 转发进来的 q 落到
		// 默认分支视为无操作。Tab 层 MUST NOT 直接 tea.Quit。
	}
	return t, nil
}

func (t *historyTab) current() *provider.HistoryVersion {
	idx := t.list.Cursor()
	if idx < 0 || idx >= len(t.rows) {
		return nil
	}
	return &t.rows[idx]
}

func (t *historyTab) doRestore(v provider.HistoryVersion) tea.Cmd {
	source := t.source
	return func() tea.Msg {
		return historyRestoredMsg{err: source.Restore(context.Background(), v)}
	}
}

func (t *historyTab) SetSize(width, height int) {
	t.width, t.height = width, height
	// 4 行 chrome：面板标题 1 行 + 附加行（detail/confirm/flash）预留 + 边距。
	// 仅作翻页步长预算；渲染窗口按 View 内实际附加行高度精算。
	t.list.SetHeight(height - 4)
}

func (t *historyTab) View() string {
	// 最小终端（30×7）下内容区高度恰为 0：不渲染，避免任何溢出。
	if t.height <= 0 || t.width <= 0 {
		return ""
	}
	// 加载/空态与列表同用固定面板几何（撑满内容区），切换无布局跳动。
	if !t.loaded {
		return t.plainPane("loading history…")
	}
	if len(t.rows) == 0 {
		title := "vault recent history"
		if t.entryID != "" {
			title = "entry " + t.entryID
		}
		return t.plainPane(title + "\n\n(no history versions: the entry was never modified, or the server has history retention disabled)")
	}

	// 面板边框画在 Width 之外（与其他 tab 的双栏预算一致）：内容区宽
	// t.width 时面板 Width 必须留出 2 列边框，否则圆角边框在 frame 内折行。
	paneW := maxInt(t.width-2, 4)
	innerW := maxInt(paneW-2, 8)
	title := "vault recent history (enter to view one entry)"
	if t.entryID != "" {
		title = fmt.Sprintf("version timeline for %s", t.entryID)
	}
	lines := make([]string, 0, len(t.rows))
	for i, row := range t.rows {
		line := fmt.Sprintf("%-20s %-18s rev %-4d %s",
			row.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			truncateRunes(entryIDOf(row), 18), row.Revision,
			truncateRunes(t.previewOf(row), 30))
		if i == t.list.Cursor() && t.mode != historyModeDetail {
			line = selectedLineStyle.Render("> " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, truncateWidth(line, innerW))
	}

	// 附加行渲染在面板内部（确认/详情/flash），空行分隔计入高度预算。
	var extraRows []string
	if t.mode == historyModeDetail {
		extraRows = append(extraRows, "")
		extraRows = append(extraRows, fitLines(strings.Split(t.detailView(), "\n"), innerW)...)
	}
	if t.mode == historyModeConfirm {
		if row := t.current(); row != nil {
			// 可操作部分前置：窄终端截断后 [y/N] 仍可见。
			extraRows = append(extraRows, "", truncateWidth(fmt.Sprintf(
				"restore to revision %d? [y/N] — writes back the historical value and pushes (creates a new revision)",
				row.Revision), innerW))
		}
	}
	if t.flash != "" {
		extraRows = append(extraRows, "", historyFlashStyle.Render(truncateWidth(t.flash, innerW)))
	}

	listH := t.height - len(extraRows)
	if listH < 1 {
		listH = 1
	}
	out := windowedPane(title, lines, t.list.Cursor(), listH, paneW)
	if len(extraRows) > 0 {
		out = out + "\n" + strings.Join(extraRows, "\n")
	}
	out = clipLines(out, t.height)
	return paneStyle.Width(paneW).Height(t.height).Render(out)
}

// plainPane 把单段提示文本渲染进撑满内容区的固定面板。
func (t *historyTab) plainPane(text string) string {
	paneW := maxInt(t.width-2, 4)
	lines := strings.Split(text, "\n")
	// emptyStateStyle 自带 Padding(1,2)：文本上限再让出 4 列。
	innerW := maxInt(paneW-6, 8)
	for i, l := range lines {
		lines[i] = truncateWidth(l, innerW)
	}
	return paneStyle.Width(paneW).Height(t.height).Render(emptyStateStyle.Render(strings.Join(lines, "\n")))
}

func (t *historyTab) previewOf(row provider.HistoryVersion) string {
	if row.Deleted {
		return "(deleted record)"
	}
	text, err := t.source.DecryptHistory(row)
	if err != nil {
		return "<cannot decrypt>"
	}
	one := strings.ReplaceAll(strings.TrimSpace(text), "\n", "⏎")
	return truncateRunes(one, 40)
}

func (t *historyTab) detailView() string {
	row := t.current()
	if row == nil {
		return ""
	}
	text, err := t.source.DecryptHistory(*row)
	if err != nil {
		return fmt.Sprintf("cannot decrypt this version: %v", err)
	}
	if len(text) > 4096 {
		text = text[:4096] + "…(truncated)"
	}
	return fmt.Sprintf("revision %d @ %s\n%s",
		row.Revision, row.CreatedAt.Local().Format("2006-01-02 15:04:05"), text)
}

// historyFlashStyle 渲染恢复成功提示（复用 tab 内局部样式）
var historyFlashStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
