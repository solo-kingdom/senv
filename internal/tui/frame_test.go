package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// sizedModel builds a Model and dispatches a WindowSizeMsg so that View()
// produces the full framed layout.
func sizedModel(w, h int) Model {
	m := New(Managers{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return out.(Model)
}

// runeLen returns the number of runes in s, ignoring a trailing carriage
// return that some renderers append.
func runeLen(s string) int { return len([]rune(strings.TrimRight(s, "\r"))) }

// TestOuterFramePresent verifies the TUI is wrapped in a rounded border that
// fills the terminal: the first row starts with ╭ and the last row contains ╰.
func TestOuterFramePresent(t *testing.T) {
	m := sizedModel(80, 24)
	out := m.View()
	lines := strings.Split(out, "\n")

	if len(lines) == 0 {
		t.Fatal("View() produced no output")
	}
	if !strings.HasPrefix(lines[0], "╭") {
		t.Errorf("first row does not start with ╭: %q", clipRunesT(lines[0], 20))
	}
	if !strings.Contains(lines[0], "╮") {
		t.Errorf("first row does not end with ╮: %q", clipRunesT(lines[0], 20))
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "╰") {
		t.Errorf("last row does not contain ╰: %q", clipRunesT(last, 20))
	}
	for i, ln := range lines {
		if r := runeLen(ln); r > 80 {
			t.Errorf("row %d has %d runes, want <= 80", i, r)
		}
	}
}

// TestActiveTabHasBackgroundBlock verifies the active tab label is rendered
// with a background colour block while inactive tabs are not.
func TestActiveTabHasBackgroundBlock(t *testing.T) {
	// Force a colour profile so lipgloss emits ANSI escape sequences
	// (without a TTY lipgloss strips all colour).
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.DefaultRenderer().SetColorProfile(termenv.ANSI256)
	defer lipgloss.DefaultRenderer().SetColorProfile(prev)

	// Active = 0 (Env): "Env" should carry a background escape, others not.
	m := sizedModel(80, 24)
	out := m.View()
	assertHasBackgroundAround(t, out, "Env", true)
	assertHasBackgroundAround(t, out, "Text", false)
	assertHasBackgroundAround(t, out, "Config", false)

	// Switch to tab 1 (Text) and verify the background moves.
	m.active = 1
	out = m.View()
	assertHasBackgroundAround(t, out, "Env", false)
	assertHasBackgroundAround(t, out, "Text", true)
}

// assertHasBackgroundAround checks whether the given label is rendered inside
// a background-coloured block. It looks at the styling segment between the last
// ANSI reset (\x1b[0m) before the label and the label itself.
func assertHasBackgroundAround(t *testing.T, out, label string, want bool) {
	t.Helper()
	idx := strings.Index(out, label)
	if idx < 0 {
		t.Fatalf("label %q not found in View() output", label)
	}
	beforeLabel := out[:idx]
	// Narrow to the styling segment after the most recent reset so adjacent
	// labels' escape sequences don't bleed into the check.
	lastReset := strings.LastIndex(beforeLabel, "\x1b[0m")
	segment := beforeLabel
	if lastReset >= 0 {
		segment = beforeLabel[lastReset:]
	}
	// lipgloss v1.x combines attributes into one SGR escape (e.g.
	// \x1b[1;38;5;231;48;5;99m). Detect the background attribute "48;5;"
	// regardless of where it sits in the combined sequence.
	hasBg := strings.Contains(segment, "48;5;")
	if hasBg != want {
		t.Errorf("label %q: hasBackground=%v, want %v (segment=%q)", label, hasBg, want, segment)
	}
}

// TestTabSeparatorsPresent verifies exactly two separator characters (│)
// appear between the three tab labels in the tab strip row.
func TestTabSeparatorsPresent(t *testing.T) {
	m := sizedModel(80, 24)
	out := m.View()
	lines := strings.Split(out, "\n")

	// The tab strip row is the one containing all three tab titles.
	var tabRow string
	for _, ln := range lines {
		if strings.Contains(ln, "Env") && strings.Contains(ln, "Text") && strings.Contains(ln, "Config") {
			tabRow = ln
			break
		}
	}
	if tabRow == "" {
		t.Fatal("no row contains all three tab labels (Env/Text/Config)")
	}

	// Extract the segment between "Env" and "Config" and count separators.
	envIdx := strings.Index(tabRow, "Env")
	configIdx := strings.Index(tabRow, "Config")
	if envIdx < 0 || configIdx < 0 || configIdx <= envIdx {
		t.Fatalf("cannot locate Env/Config in tab row: %q", tabRow)
	}
	segment := tabRow[envIdx:configIdx]
	sep := strings.Count(segment, "│")
	if sep != 2 {
		t.Errorf("tab separators between Env and Config = %d, want 2 (row: %q)", sep, clipRunesT(tabRow, 60))
	}
}

// TestMinSizeGuard verifies that when the terminal is too small, View()
// returns a plain hint with no box-drawing characters.
func TestMinSizeGuard(t *testing.T) {
	cases := []struct{ w, h int }{
		{20, 10}, // width too small
		{80, 5},  // height too small
	}
	for _, c := range cases {
		m := New(Managers{})
		m.width, m.height = c.w, c.h
		out := m.View()
		if !strings.Contains(out, "terminal too small") {
			t.Errorf("%dx%d: expected 'terminal too small' hint, got %q", c.w, c.h, clipRunesT(out, 40))
		}
		if strings.ContainsAny(out, "╭╮╰╯│─") {
			t.Errorf("%dx%d: guard output contains box-drawing chars: %q", c.w, c.h, clipRunesT(out, 40))
		}
	}
}

// TestContentDoesNotOverflowFrame verifies every row fits within the terminal
// width and the total row count equals the terminal height.
func TestContentDoesNotOverflowFrame(t *testing.T) {
	m := sizedModel(80, 24)
	out := m.View()
	lines := strings.Split(out, "\n")

	if len(lines) != 24 {
		t.Errorf("row count = %d, want exactly 24", len(lines))
	}
	for i, ln := range lines {
		r := runeLen(ln)
		if r > 80 {
			t.Errorf("row %d has %d runes, exceeds 80 (frame overflow)", i, r)
		}
		// The first and last column should be frame border characters for
		// every row (no content leaks past the border).
		runes := []rune(ln)
		if len(runes) > 0 && i > 0 && i < len(lines)-1 {
			if string(runes[0]) != "│" {
				t.Errorf("row %d col 0 = %q, want │ (left frame border)", i, string(runes[0]))
			}
		}
	}
}

// clipRunesT truncates a string for logging in test messages.
func clipRunesT(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
