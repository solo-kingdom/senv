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

// TestActiveTabHasAccentPill verifies the active tab uses the accent pill
// (bold + background) and that switching tabs moves the pill.
func TestActiveTabHasAccentPill(t *testing.T) {
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.DefaultRenderer().SetColorProfile(termenv.ANSI256)
	defer lipgloss.DefaultRenderer().SetColorProfile(prev)

	active := activeTabStyle.Render("Env")
	inactive := tabStyle.Render("Env")
	if !strings.Contains(active, "48;") && !strings.Contains(active, "[48;") {
		t.Fatalf("activeTabStyle missing background SGR: %q", active)
	}
	if strings.Contains(inactive, "48;") || strings.Contains(inactive, "[48;") {
		t.Fatalf("tabStyle should not set background: %q", inactive)
	}
	if strings.Contains(active, ";4;") || strings.Contains(active, "[4m") || strings.Contains(active, "[4;") {
		t.Fatalf("activeTabStyle must not underline: %q", active)
	}

	m := sizedModel(80, 24)
	out := m.View()
	if out == "" || out == "starting…" {
		t.Skip("View() not ready (needs window size)")
	}
	if !strings.Contains(out, activeTabStyle.Render("Env")) {
		t.Fatalf("View missing active Env pill")
	}
	m.active = 1
	out = m.View()
	if !strings.Contains(out, activeTabStyle.Render("Text")) {
		t.Fatalf("View missing active Text pill after switch")
	}
	if strings.Contains(out, activeTabStyle.Render("Env")) {
		t.Fatalf("View still has Env pill after switching to Text")
	}
}

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
