package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
		if d.top < len(d.lines)-1 {
			d.top++
		}
	case "pgup":
		d.top -= d.pageSize()
		if d.top < 0 {
			d.top = 0
		}
	case "pgdown":
		d.top += d.pageSize()
		if d.top > len(d.lines)-1 {
			d.top = maxInt(len(d.lines)-1, 0)
		}
	}
	return d, nil
}

// pageSize is the number of content rows that fit in the overlay body. The
// overlay box owns the tab's whole pane slot (d.height+2 rows incl. borders,
// 4 of them overlay chrome), and the body chrome (title, two blanks, status
// bar) takes another 4.
func (d *detailOverlay) pageSize() int {
	if d.height <= 8 {
		return 1
	}
	return d.height - 8
}

func (d *detailOverlay) View() string {
	page := d.pageSize()
	start := clamp(d.top, 0, maxInt(len(d.lines)-1, 0))
	end := start + page
	if end > len(d.lines) {
		end = len(d.lines)
	}
	title := d.title
	if len(d.lines) > page && page > 0 {
		title = fmt.Sprintf("%s  %d–%d/%d", d.title, start+1, end, len(d.lines))
	}
	body := ""
	if end > start {
		body = strings.Join(d.lines[start:end], "\n")
	}
	box := searchOverlayStyle
	if d.width > 14 {
		// Bound the box so long values wrap inside it instead of pushing the
		// frame wider: overlay chrome is 6 cols, so Width(d.width-6) makes the
		// rendered box exactly d.width (the pane slot's width).
		box = box.Width(d.width - 6)
	}
	out := box.Render(lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render(title), "", body, "",
		statusBarStyle.Render("↑↓/PgUp/PgDn scroll · esc close")))
	return clipLines(out, maxInt(d.height+2, 2))
}
