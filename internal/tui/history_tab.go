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

	rows    []provider.HistoryVersion
	cursor  int
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

func (t *historyTab) Help() string {
	switch t.mode {
	case historyModeEntry:
		return "↑↓/jk 移动 · enter 查看 · r 恢复 · esc 返回"
	case historyModeDetail:
		return "r 恢复 · esc 返回"
	case historyModeConfirm:
		return "enter/y 确认恢复 · esc/n 取消"
	default:
		return "↑↓/jk 移动 · enter 单条目历史 · r 恢复"
	}
}

func (t *historyTab) InputMode() bool { return t.mode == historyModeConfirm }

func (t *historyTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load("")
}

func (t *historyTab) load(entryID string) tea.Cmd {
	source := t.source
	return func() tea.Msg {
		f := provider.HistoryFilter{Limit: 50}
		if entryID != "" {
			kind, grp, key, ok := splitEntryID(entryID)
			if !ok {
				return historyLoadedMsg{err: fmt.Errorf("无效条目标识 %q", entryID)}
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
		t.cursor = 0
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
		t.flash = "✓ 已恢复并推送（产生新 revision）"
		t.mode = historyModeEntry
		t.loaded = true
		return t, t.load(t.entryID)
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				if t.cursor > 0 {
					t.cursor--
				}
				return t, nil
			}
		case "down", "j":
			if t.mode == historyModeRecent || t.mode == historyModeEntry {
				if t.cursor < len(t.rows)-1 {
					t.cursor++
				}
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
		case "r":
			if t.mode == historyModeRecent || t.mode == historyModeEntry || t.mode == historyModeDetail {
				if row := t.current(); row != nil && !row.Deleted {
					t.mode = historyModeConfirm
					return t, nil
				}
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
		case "q":
			// q 在顶层处理（退出程序）；确认模式下 InputMode=true 会把 q 转发到这里
			if t.mode != historyModeConfirm {
				return t, tea.Quit
			}
			return t, nil
		}
	}
	return t, nil
}

func (t *historyTab) current() *provider.HistoryVersion {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return nil
	}
	return &t.rows[t.cursor]
}

func (t *historyTab) doRestore(v provider.HistoryVersion) tea.Cmd {
	source := t.source
	return func() tea.Msg {
		return historyRestoredMsg{err: source.Restore(context.Background(), v)}
	}
}

func (t *historyTab) SetSize(width, height int) { t.width, t.height = width, height }

// visibleRows 简单窗口化：光标尽量居中，列表不超过可用行数
func (t *historyTab) visibleRows() ([]provider.HistoryVersion, int) {
	h := t.height - 4 // 表头 + 边距 + flash 行
	if h < 1 {
		h = 1
	}
	top := t.cursor - h/2
	if top > len(t.rows)-h {
		top = len(t.rows) - h
	}
	if top < 0 {
		top = 0
	}
	end := top + h
	if end > len(t.rows) {
		end = len(t.rows)
	}
	return t.rows[top:end], top
}

func (t *historyTab) View() string {
	if !t.loaded {
		return "加载历史中…"
	}
	if len(t.rows) == 0 {
		title := "vault 最近历史变更"
		if t.entryID != "" {
			title = "条目 " + t.entryID
		}
		return paneStyle.Render(fmt.Sprintf("%s\n\n（无历史版本：条目未被修改过，或 server 未开启历史留存）", title))
	}

	rows, top := t.visibleRows()
	var b strings.Builder
	if t.entryID == "" {
		b.WriteString("vault 最近历史变更（enter 查看单条目）\n\n")
	} else {
		b.WriteString(fmt.Sprintf("条目 %s 的版本时间线\n\n", t.entryID))
	}
	for i, row := range rows {
		idx := top + i
		prefix := "  "
		line := fmt.Sprintf("%-20s %-18s rev %-4d %s",
			row.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			truncateRunes(entryIDOf(row), 18), row.Revision,
			truncateRunes(t.previewOf(row), 30))
		if idx == t.cursor && t.mode != historyModeDetail {
			prefix = "> "
			line = selectedLineStyle.Render(line)
		}
		b.WriteString(prefix + line + "\n")
	}
	if t.mode == historyModeDetail {
		b.WriteString("\n" + t.detailView())
	}
	if t.mode == historyModeConfirm {
		row := t.current()
		if row != nil {
			b.WriteString("\n确认恢复到 revision " + fmt.Sprint(row.Revision) +
				"？（写回历史值并推送，产生新 revision）[y/N] ")
		}
	}
	if t.flash != "" {
		b.WriteString("\n" + historyFlashStyle.Render(t.flash))
	}
	return paneStyle.Render(b.String())
}

func (t *historyTab) previewOf(row provider.HistoryVersion) string {
	if row.Deleted {
		return "(删除记录)"
	}
	text, err := t.source.DecryptHistory(row)
	if err != nil {
		return "<无法解密>"
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
		return fmt.Sprintf("无法解密该版本：%v", err)
	}
	if len(text) > 4096 {
		text = text[:4096] + "…（截断）"
	}
	return fmt.Sprintf("revision %d @ %s\n%s",
		row.Revision, row.CreatedAt.Local().Format("2006-01-02 15:04:05"), text)
}

// historyFlashStyle 渲染恢复成功提示（复用 tab 内局部样式）
var historyFlashStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
