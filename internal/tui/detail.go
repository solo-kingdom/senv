package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// detailOverlay shows the full text of a selected entry. Panes truncate their
// rows so a long value can never widen or wrap them; `enter` opens this overlay
// to read the whole thing (and to scroll it).
type detailOverlay struct {
	title         string
	lines         []string
	top           int
	width, height int
}

// detailCloseMsg asks the owning tab to close the overlay.
type detailCloseMsg struct{}

func newDetailOverlay(title string, lines []string) *detailOverlay {
	return &detailOverlay{title: title, lines: lines}
}

func (d *detailOverlay) SetSize(w, h int) { d.width, d.height = w, h }

// Update scrolls the overlay; `esc`/`enter`/`q` close it.
func (d *detailOverlay) Update(msg tea.Msg) (*detailOverlay, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch key.String() {
	case "esc", "enter", "q":
		return d, func() tea.Msg { return detailCloseMsg{} }
	case "up", "k":
		if d.top > 0 {
			d.top--
		}
	case "down", "j":
		if d.top < d.maxTop() {
			d.top++
		}
	case "pgup":
		d.top -= d.pageSize()
		if d.top < 0 {
			d.top = 0
		}
	case "pgdown":
		d.top += d.pageSize()
		if d.top > d.maxTop() {
			d.top = d.maxTop()
		}
	}
	return d, nil
}

// pageSize is the number of content rows that fit in the overlay body. The
// overlay owns the tab's whole pane slot — the same d.width × (d.height+2) box
// every tab pane renders — so the frame does not shift when it opens. Of those
// rows, 4 are overlay chrome (border + vertical padding) and 4 are fixed body
// chrome (title, blank, blank, status bar), leaving d.height-6 scrolling rows.
func (d *detailOverlay) pageSize() int {
	if d.height <= 6 {
		return 1
	}
	return d.height - 6
}

// bodyWidth is the text width inside the overlay box (2 border + 2×2 padding
// columns). Zero means the box is unconstrained (very narrow terminals).
func (d *detailOverlay) bodyWidth() int {
	if d.width <= 14 {
		return 0
	}
	return d.width - 6
}

// displayLines 把原文行变成可直接画进弹层的终端安全行：制表符展开、控制/
// 转义字符剔除（sanitizeLine），超宽行按显示列硬折行。滚动与页码都按折行
// 后的显示行计——否则 lipgloss Width 自动折行会让框比预算更高，底部被裁掉。
func (d *detailOverlay) displayLines() []string {
	w := d.bodyWidth()
	out := make([]string, 0, len(d.lines))
	for _, l := range d.lines {
		l = sanitizeLine(strings.ReplaceAll(l, "\t", "    "))
		if w <= 0 || ansi.StringWidth(l) <= w {
			out = append(out, l)
			continue
		}
		out = append(out, strings.Split(ansi.Hardwrap(l, w, true), "\n")...)
	}
	return out
}

// maxTop is the start row of the last full page: scrolling stops with the final
// line at the bottom edge instead of leaving one line above a blank tail.
func (d *detailOverlay) maxTop() int {
	return maxInt(len(d.displayLines())-d.pageSize(), 0)
}

func (d *detailOverlay) View() string {
	lines := d.displayLines()
	page := d.pageSize()
	start := clamp(d.top, 0, d.maxTop())
	end := start + page
	if end > len(lines) {
		end = len(lines)
	}
	title := plainText(d.title)
	if len(lines) > page && page > 0 {
		title = fmt.Sprintf("%s  %d–%d/%d", title, start+1, end, len(lines))
	}
	if w := d.bodyWidth(); w > 0 {
		title = truncateWidth(title, w)
	}
	body := make([]string, 0, page)
	if end > start {
		body = append(body, lines[start:end]...)
	}
	// 补齐到整页：框高固定，滚动时下边缘不动（内容不足一页时也不缩框）。
	for len(body) < page {
		body = append(body, "")
	}
	box := searchOverlayStyle
	if d.width > 14 {
		// lipgloss v1 的 Width 不含边框：留 2 列给边框，整框宽度 = d.width。
		box = box.Width(d.width - 2)
	}
	out := box.Render(lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render(title), "",
		strings.Join(body, "\n"), "",
		statusBarStyle.Render("↑↓/PgUp/PgDn scroll · esc close")))
	return clipLines(out, maxInt(d.height+2, 2))
}
