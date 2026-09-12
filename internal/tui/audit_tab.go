package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
)

// AuditSource 是 Audit Tab 的数据源（cmd 层注入本机审计读取器）
type AuditSource interface {
	LoadAuditEvents() ([]session.AuditEntry, int, error)
}

// auditFilterPreset 是 'f' 键循环的过滤预设
var auditFilterPresets = []struct {
	label string
	match func(session.AuditEntry) bool
}{
	{"all", func(session.AuditEntry) bool { return true }},
	{"ops", func(e session.AuditEntry) bool { return strings.HasPrefix(string(e.EventType), "op_") }},
	{"sessions", func(e session.AuditEntry) bool { return !strings.HasPrefix(string(e.EventType), "op_") }},
}

// auditTab 渲染本机审计事件时间线（含日期时间、操作、目标、结果）
type auditTab struct {
	source        AuditSource
	width, height int
	loaded        bool

	rows      []session.AuditEntry
	skipped   int
	filterIdx int
	// filterBox 是 'f' 预设之外的自由文本过滤状态机（匹配 event type /
	// target / message）；输入态下键盘进入过滤器而不是移动光标。
	filterBox Filter
	// list 承载游标与窗口（共享列表组件），替代手写 top/pageSize/clampWindow。
	list    List
	loadErr string
}

type auditLoadedMsg struct {
	rows    []session.AuditEntry
	skipped int
	err     error
}

func newAuditTab(source AuditSource) *auditTab {
	return &auditTab{source: source}
}

func (t *auditTab) Title() string { return "Audit" }

func (t *auditTab) Bindings() []KeyAction {
	return []KeyAction{
		actUp, actDown, actPageUp, actPageDn, actTop, actBottom,
		{[]string{"f"}, "preset filter (" + auditFilterPresets[t.filterIdx].label + ")", grpFilter},
		actFilter, actRefresh,
	}
}

func (t *auditTab) InputMode() bool { return t.filterBox.Active() }

func (t *auditTab) Init() tea.Cmd {
	return t.load()
}

// Reload re-reads the local audit file; the top level calls it after a
// background sync applies remote changes (new audit rows may have appeared).
func (t *auditTab) Reload() tea.Cmd {
	return t.load()
}

func (t *auditTab) load() tea.Cmd {
	source := t.source
	return func() tea.Msg {
		st := perflog.Start("tui.load-audit")
		rows, skipped, err := source.LoadAuditEvents()
		st.With("rows", len(rows)).EndErr(err)
		return auditLoadedMsg{rows: rows, skipped: skipped, err: err}
	}
}

func (t *auditTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case auditLoadedMsg:
		t.loaded = true
		t.loadErr = ""
		if msg.err != nil {
			t.loadErr = msg.err.Error()
			return t, nil
		}
		t.rows = msg.rows
		t.skipped = msg.skipped
		t.clampCursor()
		return t, nil

	case tea.KeyMsg:
		// 自由文本过滤（共享 Filter 状态机）：live match、esc 清词、enter 确认；
		// 匹配谓词保留 audit 专属的 auditEntryMatches（比 matchKey 多匹配 message）。
		if t.filterBox.Active() {
			switch msg.String() {
			case "esc":
				t.filterBox.Clear()
				t.clampCursor()
				return t, nil
			case "enter":
				t.filterBox.Confirm()
				return t, nil
			case "backspace":
				t.filterBox.Backspace()
				t.clampCursor()
				return t, nil
			}
			if isPrintable(msg) {
				t.filterBox.Append(msg.String())
				t.clampCursor()
			}
			return t, nil
		}

		switch msg.String() {
		case "/":
			t.filterBox.Enter()
			t.clampCursor()
			return t, nil
		case "up", "k":
			t.list.Move(-1, len(t.filtered()))
		case "down", "j":
			t.list.Move(1, len(t.filtered()))
		case "pgup":
			t.list.Page(-1, len(t.filtered()))
		case "pgdown":
			t.list.Page(1, len(t.filtered()))
		case "f":
			t.filterIdx = (t.filterIdx + 1) % len(auditFilterPresets)
			t.clampCursor()
		case "ctrl+r":
			return t, t.load()
		case "g":
			t.list.Home()
		case "G":
			t.list.End(len(t.filtered()))
		}
	}
	return t, nil
}

func (t *auditTab) filtered() []session.AuditEntry {
	match := auditFilterPresets[t.filterIdx].match
	needle := strings.ToLower(strings.TrimSpace(t.filterBox.Term()))
	out := make([]session.AuditEntry, 0, len(t.rows))
	for _, e := range t.rows {
		if !match(e) {
			continue
		}
		if needle != "" && !auditEntryMatches(e, needle) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// auditEntryMatches 只在非敏感字段（类型、目标、详情）上做大小写不敏感匹配。
func auditEntryMatches(e session.AuditEntry, needle string) bool {
	haystack := strings.ToLower(string(e.EventType) + "\n" + e.Target + "\n" + e.Message)
	return strings.Contains(haystack, needle)
}

func (t *auditTab) clampCursor() {
	t.list.SetCursor(t.list.Cursor(), len(t.filtered()))
}

func (t *auditTab) SetSize(width, height int) {
	t.width, t.height = width, height
	// 4 行 chrome：面板标题 1 行 + 提示/过滤附加行预留 + 边距。
	// 仅作翻页步长预算；渲染窗口按 View 内实际附加行高度精算。
	t.list.SetHeight(height - 4)
}

func (t *auditTab) View() string {
	// 最小终端（30×7）下内容区高度恰为 0：不渲染，避免任何溢出。
	if t.height <= 0 || t.width <= 0 {
		return ""
	}
	// 加载/错误/空态与列表同用固定面板几何（撑满内容区），切换无布局跳动。
	if t.loadErr != "" {
		return t.plainPane("failed to load audit log: " + t.loadErr)
	}
	if !t.loaded {
		return t.plainPane("loading audit log…")
	}
	rows := t.filtered()
	if len(rows) == 0 {
		if t.filterBox.Term() != "" {
			return t.plainPane("(no events matching /" + t.filterBox.Term() + ")")
		}
		return t.plainPane("(no audit events yet)")
	}

	innerW := maxInt(t.width-4, 8)
	filterLabel := auditFilterPresets[t.filterIdx].label
	if t.filterBox.Term() != "" {
		filterLabel += " + /" + t.filterBox.Term()
	}
	title := truncateWidth(fmt.Sprintf("local audit events (filter: %s, %d total, newest first)",
		filterLabel, len(rows)), innerW)
	lines := make([]string, 0, len(rows))
	for i, e := range rows {
		outcome := "✓"
		if !e.Success {
			outcome = "✗"
		}
		line := fmt.Sprintf("%s  %-18s %-8s %s",
			e.Timestamp.Local().Format("2006-01-02 15:04:05"),
			string(e.EventType), outcome, e.Target)
		if i == t.list.Cursor() {
			line = selectedLineStyle.Render("> " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, truncateWidth(line, innerW))
	}

	// 附加行渲染在面板内部（skipped/过滤输入），空行分隔计入高度预算。
	var extraRows []string
	if t.skipped > 0 {
		extraRows = append(extraRows, "", truncateWidth(
			fmt.Sprintf("⚠ skipped %d unparseable records", t.skipped), innerW))
	}
	if t.filterBox.Active() {
		extraRows = append(extraRows, "", truncateWidth(
			t.filterBox.Prompt()+"(enter confirm · esc clear)", innerW))
	}

	listH := t.height - len(extraRows)
	if listH < 1 {
		listH = 1
	}
	out := windowedPane(title, lines, t.list.Cursor(), listH, t.width)
	if len(extraRows) > 0 {
		out = out + "\n" + strings.Join(extraRows, "\n")
	}
	out = clipLines(out, t.height)
	return paneStyle.Width(t.width).Height(t.height).Render(out)
}

// plainPane 把单段提示文本渲染进撑满内容区的固定面板。
func (t *auditTab) plainPane(text string) string {
	lines := strings.Split(text, "\n")
	innerW := maxInt(t.width-8, 8)
	for i, l := range lines {
		lines[i] = truncateWidth(l, innerW)
	}
	return paneStyle.Width(t.width).Height(t.height).Render(emptyStateStyle.Render(strings.Join(lines, "\n")))
}
