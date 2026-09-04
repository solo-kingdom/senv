package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// listPageSize is the number of list rows that fit under a 1-line pane title.
// A non-positive height means "no window" (show everything).
func listPageSize(height int) int {
	if height <= 1 {
		return 0
	}
	return height - 1
}

// visibleRange returns a half-open [start, end) window of n items that includes
// cursor and is at most page rows. page <= 0 shows the whole list.
func visibleRange(n, cursor, page int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	if page <= 0 || n <= page {
		return 0, n
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= n {
		cursor = n - 1
	}
	start = cursor - page + 1
	if start < 0 {
		start = 0
	}
	if start+page > n {
		start = n - page
	}
	return start, start + page
}

// clipLines keeps at most n lines of s (no-op when n <= 0). Used as a last
// line of defence so a pane cannot push the rest of the TUI off-screen.
func clipLines(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// windowedPane renders a titled list that stays within height rows, scrolling
// so that cursor remains visible. When the list is longer than the pane, the
// title shows the visible 1-based range (e.g. "Groups (12)  4–12"). width is
// the pane Width; the title is truncated so lipgloss Width-wrap cannot add
// extra rows (title style padding plus pane padding consume 4 columns).
func windowedPane(title string, lines []string, cursor, height, width int) string {
	page := listPageSize(height)
	start, end := visibleRange(len(lines), cursor, page)
	if start > 0 || end < len(lines) {
		title = fmt.Sprintf("%s  %d–%d", title, start+1, end)
	}
	if width > 4 {
		title = truncateRunes(title, width-4)
	}
	parts := make([]string, 0, 1+end-start)
	parts = append(parts, paneTitleStyle.Render(title))
	if end > start {
		parts = append(parts, lines[start:end]...)
	}
	out := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if height > 0 {
		out = clipLines(out, height)
	}
	return out
}
