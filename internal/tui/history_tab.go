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
// 时间线）→ detail（共享滚动弹层看选中版本全文）→ confirm（恢复确认）。
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
	// detail 是滚动详情弹层（共享组件）：选中版本解密后的全文在这里看。
	// 打开时接管全部按键与渲染——早期实现把正文当附加行追加在列表下方，
	// 会被面板底边裁掉且不能滚动，长值（私钥、长 token）尾部不可读。
	detail *detailOverlay
}

type historyMode int

const (
	historyModeRecent historyMode = iota
	historyModeEntry
	historyModeConfirm
)

// historyTimeLayout 是历史版本时间戳的展示格式（列表与详情共用）。
const historyTimeLayout = "2006-01-02 15:04:05"

// historyTimeOf 按本地时区格式化版本时间戳。
func historyTimeOf(v provider.HistoryVersion) string {
	return v.CreatedAt.Local().Format(historyTimeLayout)
}

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
	if t.detail != nil {
		return detailBindings()
	}
	switch t.mode {
	case historyModeEntry:
		return append(navBindings(false), actDetail,
			KeyAction{[]string{"R"}, "restore", grpItem, false},
			actEsc,
		)
	case historyModeConfirm:
		return []KeyAction{
			{[]string{"enter/y"}, "confirm", grpConfirm, false},
			{[]string{"esc/n"}, "cancel", grpConfirm, false},
		}
	default:
		return append(navBindings(false),
			KeyAction{[]string{"enter"}, "open entry", grpItem, false},
			KeyAction{[]string{"R"}, "restore", grpItem, false},
		)
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
		if t.mode == historyModeConfirm {
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

	case detailCloseMsg:
		t.detail = nil
		return t, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		if t.detail != nil {
			var cmd tea.Cmd
			t.detail, cmd = t.detail.Update(msg)
			return t, cmd
		}
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
			case historyModeEntry:
				t.openDetail()
				return t, nil
			case historyModeRecent:
				if row := t.current(); row != nil && !row.Deleted {
					id := entryIDOf(*row)
					t.mode = historyModeEntry
					return t, t.load(id)
				}
			}
		case "R":
			// R=恢复（r 已统一为重命名语义；grill D7）
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
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
	// 4 行 chrome：面板标题 1 行 + 附加行（confirm/flash）预留 + 边距。
	// 仅作翻页步长预算；渲染窗口按 View 内实际附加行高度精算。
	t.list.SetHeight(height - 4)
	if t.detail != nil {
		// 详情弹层顶替整个面板位，终端尺寸变化时跟随重排。
		t.detail.SetSize(width, height)
	}
}

func (t *historyTab) View() string {
	// 最小终端（30×7）下内容区高度恰为 0：不渲染，避免任何溢出。
	if t.height <= 0 || t.width <= 0 {
		return ""
	}
	if t.detail != nil {
		return t.detail.View()
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
	// 列宽按面板实际宽度分配：固定列（游标 2 + 时间 + 两处分隔 + rev 列 7 +
	// 末尾分隔）之外的余量按约 1:2 分给条目名与预览。早期实现把两列钉死在
	// 18/30 rune，宽终端下整行只用到约一半宽度就截断。
	tsW := lipgloss.Width(historyTimeOf(t.rows[0]))
	flex := maxInt(innerW-(2+tsW+1+7+1+1), 12)
	entryW := clamp(flex/3, 8, 36)
	previewW := maxInt(flex-entryW, 4)
	lines := make([]string, 0, len(t.rows))
	for i, row := range t.rows {
		body := padRight(historyTimeOf(row), tsW) + " " +
			padRight(truncateWidth(fmt.Sprintf("rev %d", row.Revision), 6), 7) + " " +
			padRight(truncateWidth(entryIDOf(row), entryW), entryW) + " " +
			truncateWidth(t.previewOf(row), previewW)
		// 先按面板宽度截断未着色文本再上色：着色后的字符串含 ANSI 序列，
		// 截断会切坏转义（其他 Tab 的列表行同一约定）。
		body = truncateWidth(body, maxInt(innerW-2, 1))
		if i == t.list.Cursor() {
			body = selectedLineStyle.Render("> " + body)
		} else {
			body = "  " + body
		}
		lines = append(lines, body)
	}

	// 附加行渲染在面板内部（确认/flash），空行分隔计入高度预算。
	var extraRows []string
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
	// 上限只用于兜住超大值（backup 可达 512KB）的逐帧渲染成本，远大于任何
	// 终端的预览列宽；真正的列宽截断由行渲染按面板宽度做。
	return truncateRunes(one, 512)
}

// openDetail 打开滚动详情弹层，渲染选中版本的解密全文（不截断，超长可滚动）。
func (t *historyTab) openDetail() {
	row := t.current()
	if row == nil {
		return
	}
	title := fmt.Sprintf("%s · revision %d · %s",
		entryIDOf(*row), row.Revision, historyTimeOf(*row))
	t.detail = newDetailOverlay(title, t.detailLines(*row))
	t.detail.SetSize(t.width, t.height)
}

// detailLines 把选中版本解密成弹层正文行。已删除条目仍可解出最后一份密文
// （server 保留删除前的版本），因此不在这里拦截。
func (t *historyTab) detailLines(row provider.HistoryVersion) []string {
	text, err := t.source.DecryptHistory(row)
	if err != nil {
		return []string{fmt.Sprintf("cannot decrypt this version: %v", err)}
	}
	if text == "" {
		return []string{"(empty value)"}
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// historyFlashStyle 渲染恢复成功提示（复用 tab 内局部样式）
var historyFlashStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
