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

// TestActiveTabHasUnderline verifies the active tab label is rendered
// with an underline (activeTabStyle) while inactive tabs lack one.
func TestActiveTabHasUnderline(t *testing.T) {
	// Force a colour profile so lipgloss emits ANSI escape sequences
	// (without a TTY lipgloss strips all colour).
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.DefaultRenderer().SetColorProfile(termenv.ANSI256)
	defer lipgloss.DefaultRenderer().SetColorProfile(prev)

	// Active = 0 (Env): "Env" should carry an underline escape, others not.
	m := sizedModel(80, 24)
	out := m.View()
	if out == "" || out == "starting…" {
		t.Skip("View() not ready (needs window size)")
	}
	assertHasUnderlineAround(t, out, "Env", true)
	assertHasUnderlineAround(t, out, "Text", false)
	assertHasUnderlineAround(t, out, "Config", false)

	// Switch to tab 1 (Text) and verify the underline moves.
	m.active = 1
	out = m.View()
	assertHasUnderlineAround(t, out, "Env", false)
	assertHasUnderlineAround(t, out, "Text", true)
}


// assertHasUnderlineAround checks whether the given label is rendered with
// an underline. With adaptive colors, the text may have ANSI codes between
// characters, so we find the label's position in the stripped output and then
// check the original output for underline sequences.
func assertHasUnderlineAround(t *testing.T, out, label string, want bool) {
	t.Helper()
	stripped := stripANSI(out)
	idx := strings.Index(stripped, label)
	if idx < 0 {
		t.Fatalf("label %q not found in View() output", label)
	}
	// Find the approximate position in the original output by counting
	// visible characters and finding where the label starts.
	// Then check the surrounding ANSI codes for underline.
	visibleCount := 0
	origIdx := 0
	for origIdx < len(out) {
		if out[origIdx] == '\x1b' {
			// Skip ANSI sequence
			end := strings.Index(out[origIdx:], "m")
			if end >= 0 {
				origIdx += end + 1
				continue
			}
		}
		if visibleCount == idx {
			break
		}
		visibleCount++
		origIdx++
	}
	// Look back from origIdx to find the styling segment.
	beforeLabel := out[:origIdx]
	lastReset := strings.LastIndex(beforeLabel, "\x1b[0m")
	segment := beforeLabel
	if lastReset >= 0 {
		segment = beforeLabel[lastReset:]
	}
	// lipgloss v1.x uses \x1b[4m for underline. It may be combined in a
	// sequence like \x1b[1;4;38;5;...m. Look for ";4;" or "[4;" or "[4m".
	hasUnderline := strings.Contains(segment, ";4;") ||
		strings.Contains(segment, "[4;") ||
		strings.Contains(segment, "[4m")
	if hasUnderline != want {
		t.Errorf("label %q: hasUnderline=%v, want %v (segment=%q)", label, hasUnderline, want, segment)
	}
}

// TestTabStripContainsAllLabels verifies all tab labels appear in the tab strip row.
// The visual redesign uses space separators instead of │, so we just check labels are present.
func TestTabStripContainsAllLabels(t *testing.T) {
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

	// Verify labels appear in order.
	envIdx := strings.Index(tabRow, "Env")
	textIdx := strings.Index(tabRow, "Text")
	configIdx := strings.Index(tabRow, "Config")
	if envIdx < 0 || textIdx < 0 || configIdx < 0 {
		t.Fatalf("missing tab labels in row: %q", tabRow)
	}
	if !(envIdx < textIdx && textIdx < configIdx) {
		t.Errorf("tab labels not in expected order Env < Text < Config: env=%d text=%d config=%d", envIdx, textIdx, configIdx)
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
			t.Errorf("%dx%d: expected the too-small hint, got %q", c.w, c.h, clipRunesT(out, 40))
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
