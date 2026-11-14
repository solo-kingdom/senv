package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type chromeDummyTab struct {
	title         string
	width, height int
	longSelected  bool
}

func (d *chromeDummyTab) Title() string                     { return d.title }
func (d *chromeDummyTab) Init() tea.Cmd                     { return nil }
func (d *chromeDummyTab) Reload() tea.Cmd                   { return nil }
func (d *chromeDummyTab) Update(msg tea.Msg) (Tab, tea.Cmd) { return d, nil }
func (d *chromeDummyTab) InputMode() bool                   { return false }
func (d *chromeDummyTab) Bindings() []KeyAction             { return nil }
func (d *chromeDummyTab) SetSize(w, h int)                  { d.width, d.height = w, h }
func (d *chromeDummyTab) View() string {
	body := make([]string, max(d.height, 0))
	for i := range body {
		line := fmt.Sprintf("%s row %d %s", d.title, i, strings.Repeat("x", 200))
		if d.longSelected && i == 0 {
			line = selectedLineStyle.Render(truncateWidth("> "+line, maxInt(d.width-4, 8)))
		} else {
			line = truncateWidth("  "+line, maxInt(d.width-4, 8))
		}
		body[i] = line
	}
	return paneStyle.Width(maxInt(d.width-2, 4)).Height(d.height).Render(strings.Join(body, "\n"))
}

func TestModelViewExactHeight(t *testing.T) {
	tabs := []Tab{
		&chromeDummyTab{title: "Env"},
		&chromeDummyTab{title: "Text"},
		&chromeDummyTab{title: "Config"},
		&chromeDummyTab{title: "SSH"},
		&chromeDummyTab{title: "KeyPair"},
		&chromeDummyTab{title: "LLM"},
		&chromeDummyTab{title: "MCP"},
		&chromeDummyTab{title: "History", longSelected: true},
		&chromeDummyTab{title: "Audit", longSelected: true},
		&chromeDummyTab{title: "Backup"},
	}
	for _, height := range []int{24, 30, 40} {
		m := Model{tabs: tabs, active: 8, width: 100, height: height}
		w, h := computeLayout(m.width, m.height).tabSize()
		for _, tab := range m.tabs {
			tab.SetSize(w, h)
		}
		view := m.View()
		gotH := lipgloss.Height(view)
		if gotH != height {
			t.Fatalf("height=%d view=%d\n%s", height, gotH, dumpChrome(view))
		}
		plain := plainChrome(view)
		if !strings.Contains(plain, "senv") {
			t.Fatalf("top bar missing brand (height %d)\n%s", height, dumpChrome(view))
		}
		if !strings.Contains(plain, "Env") || !strings.Contains(plain, "Audit") {
			t.Fatalf("left nav missing Env/Audit (height %d)\n%s", height, dumpChrome(view))
		}
		// 激活项是 Audit：胶囊应出现一次；不得再画出第二条顶栏式 tab 条。
		navW := computeLayout(m.width, height).navW
		if !strings.Contains(view, activeNavPill("Audit", navW)) {
			t.Fatalf("missing active Audit pill (height %d)\n%s", height, dumpChrome(view))
		}
	}
}

func TestActiveTabIsSingleRowPill(t *testing.T) {
	inactive := tabStyle.Render("History")
	active := activeTabStyle.Render("Audit")
	if lipgloss.Height(inactive) != 1 || lipgloss.Height(active) != 1 {
		t.Fatalf("tab label height inactive=%d active=%d", lipgloss.Height(inactive), lipgloss.Height(active))
	}
	joined := lipgloss.JoinHorizontal(lipgloss.Top, inactive, " ", active)
	strip := tabStripStyle.Width(80).Render(joined)
	if lipgloss.Height(strip) != 2 {
		t.Fatalf("strip height=%d want 2 (label+border)\n%q", lipgloss.Height(strip), strip)
	}
}

func plainChrome(s string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			in = true
			continue
		}
		if in {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				in = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func dumpChrome(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = fmt.Sprintf("%2d|%s", i+1, plainChrome(l))
	}
	return strings.Join(lines, "\n")
}
