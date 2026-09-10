package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
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
	{"全部", func(session.AuditEntry) bool { return true }},
	{"操作", func(e session.AuditEntry) bool { return strings.HasPrefix(string(e.EventType), "op_") }},
	{"会话", func(e session.AuditEntry) bool { return !strings.HasPrefix(string(e.EventType), "op_") }},
}

// auditTab 渲染本机审计事件时间线（含日期时间、操作、目标、结果）
type auditTab struct {
	source        AuditSource
	width, height int
	loaded        bool

	rows      []session.AuditEntry
	skipped   int
	filterIdx int
	// filter 是 'f' 预设之外的自由文本过滤（匹配 event type / target / message），
	// filtering 为 true 时键盘输入进入过滤器而不是移动光标。
	filter    string
	filtering bool
	cursor    int
	top       int
	loadErr   string
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

func (t *auditTab) Help() string {
	return "↑↓/jk 移动 · PgUp/PgDn 翻页 · f 预设过滤(" + auditFilterPresets[t.filterIdx].label + ") · / 文本过滤 · r 刷新"
}

func (t *auditTab) InputMode() bool { return t.filtering }

func (t *auditTab) Init() tea.Cmd {
	return t.load()
}

func (t *auditTab) load() tea.Cmd {
	source := t.source
	return func() tea.Msg {
		rows, skipped, err := source.LoadAuditEvents()
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
		// Free-text filter input owns every key while active (mirrors the env
		// tab filter: live match, esc clears, enter keeps).
		if t.filtering {
			switch msg.String() {
			case "esc":
				t.filter = ""
				t.filtering = false
				t.clampCursor()
				return t, nil
			case "enter":
				t.filtering = false
				return t, nil
			case "backspace":
				if r := []rune(t.filter); len(r) > 0 {
					t.filter = string(r[:len(r)-1])
				}
				t.clampCursor()
				return t, nil
			}
			if isPrintable(msg) {
				t.filter += msg.String()
				t.clampCursor()
			}
			return t, nil
		}

		switch msg.String() {
		case "/":
			t.filtering = true
			t.clampCursor()
			return t, nil
		case "up", "k":
			if t.cursor > 0 {
				t.cursor--
				t.clampWindow()
			}
		case "down", "j":
			if t.cursor < len(t.filtered())-1 {
				t.cursor++
				t.clampWindow()
			}
		case "pgup":
			t.cursor -= t.pageSize()
			t.clampCursor()
		case "pgdown":
			t.cursor += t.pageSize()
			t.clampCursor()
		case "f":
			t.filterIdx = (t.filterIdx + 1) % len(auditFilterPresets)
			t.clampCursor()
		case "r":
			return t, t.load()
		}
	}
	return t, nil
}

func (t *auditTab) filtered() []session.AuditEntry {
	match := auditFilterPresets[t.filterIdx].match
	needle := strings.ToLower(strings.TrimSpace(t.filter))
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

func (t *auditTab) pageSize() int {
	h := t.height - 6
	if h < 1 {
		h = 1
	}
	return h
}

func (t *auditTab) clampCursor() {
	n := len(t.filtered())
	if t.cursor >= n {
		t.cursor = n - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	t.clampWindow()
}

func (t *auditTab) clampWindow() {
	h := t.pageSize()
	if t.top > t.cursor-h+1 {
		t.top = t.cursor - h + 1
	}
	if t.top < 0 {
		t.top = 0
	}
	if t.top > t.cursor {
		t.top = t.cursor
	}
}

func (t *auditTab) SetSize(width, height int) { t.width, t.height = width, height }

func (t *auditTab) View() string {
	if t.loadErr != "" {
		return paneStyle.Render("审计日志加载失败：" + t.loadErr)
	}
	if !t.loaded {
		return "加载审计日志中…"
	}
	rows := t.filtered()
	if len(rows) == 0 {
		if t.filter != "" {
			return paneStyle.Render(emptyStateStyle.Render("（没有匹配 /" + t.filter + " 的审计事件）"))
		}
		return paneStyle.Render(emptyStateStyle.Render("（暂无审计事件）"))
	}

	var b strings.Builder
	filterLabel := auditFilterPresets[t.filterIdx].label
	if t.filter != "" {
		filterLabel += " + /" + t.filter
	}
	b.WriteString(fmt.Sprintf("本机审计事件（过滤: %s，共 %d 条，时间新到旧）\n\n",
		filterLabel, len(rows)))
	page := t.pageSize()
	end := t.top + page
	if end > len(rows) {
		end = len(rows)
	}
	for i := t.top; i < end; i++ {
		e := rows[i]
		outcome := "✓"
		if !e.Success {
			outcome = "✗"
		}
		line := fmt.Sprintf("%s  %-18s %-8s %s",
			e.Timestamp.Local().Format("2006-01-02 15:04:05"),
			string(e.EventType), outcome, e.Target)
		prefix := "  "
		if i == t.cursor {
			prefix = "> "
			line = selectedLineStyle.Render(line)
		}
		b.WriteString(prefix + line + "\n")
	}
	if t.skipped > 0 {
		b.WriteString(fmt.Sprintf("\n⚠ 跳过 %d 行无法解析的记录\n", t.skipped))
	}
	if t.filtering {
		b.WriteString("\n/" + t.filter + "_（enter 确认 · esc 清除）\n")
	}
	return paneStyle.Render(b.String())
}
