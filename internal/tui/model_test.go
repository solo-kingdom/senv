package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// runeKey builds a KeyMsg for a rune keystroke (e.g. "2", "q", "v").
func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestNewDefaultsToEnvTab(t *testing.T) {
	m := New(Managers{})
	if m.active != 0 {
		t.Fatalf("active = %d, want 0 (Env)", m.active)
	}
	if len(m.tabs) != 3 {
		t.Fatalf("tabs = %d, want 3", len(m.tabs))
	}
	if m.tabs[0].Title() != "Env" {
		t.Fatalf("tab 0 title = %q, want Env", m.tabs[0].Title())
	}
	if m.tabs[1].Title() != "Text" || m.tabs[2].Title() != "Config" {
		t.Fatalf("unexpected tab titles: %q %q", m.tabs[1].Title(), m.tabs[2].Title())
	}
}

func TestTabSwitchByNumber(t *testing.T) {
	m := New(Managers{})

	for _, tc := range []struct {
		key  string
		want int
	}{
		{"2", 1}, // Text
		{"3", 2}, // Config
		{"1", 0}, // Env
	} {
		out, _ := m.Update(runeKey(tc.key))
		m = out.(Model)
		if m.active != tc.want {
			t.Fatalf("after pressing %q: active = %d, want %d", tc.key, m.active, tc.want)
		}
	}
}

func TestTabSwitchCycle(t *testing.T) {
	m := New(Managers{})

	// Tab key cycles forward: Env -> Text -> Config -> Env.
	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	for _, want := range []int{1, 2, 0} {
		out, _ := m.Update(tabMsg)
		m = out.(Model)
		if m.active != want {
			t.Fatalf("after Tab: active = %d, want %d", m.active, want)
		}
	}

	// Shift+Tab cycles backward: Env -> Config -> Text -> Env.
	shiftTab := tea.KeyMsg{Type: tea.KeyShiftTab}
	for _, want := range []int{2, 1, 0} {
		out, _ := m.Update(shiftTab)
		m = out.(Model)
		if m.active != want {
			t.Fatalf("after Shift+Tab: active = %d, want %d", m.active, want)
		}
	}
}

// TestTabStateIsolated verifies that switching tabs preserves each tab's
// identity (and thus its navigation state), since each tab is held by pointer.
func TestTabStateIsolated(t *testing.T) {
	m := New(Managers{})
	envBefore := m.tabs[0]

	// Switch away from Env and back.
	out, _ := m.Update(runeKey("2"))
	m = out.(Model)
	out, _ = m.Update(runeKey("1"))
	m = out.(Model)

	if m.tabs[0] != envBefore {
		t.Fatal("Env tab instance changed after round-trip; navigation state would be lost")
	}
}

func TestQuitOnQ(t *testing.T) {
	m := New(Managers{})
	_, cmd := m.Update(runeKey("q"))
	if cmd == nil {
		t.Fatal("expected a command after pressing q")
	}
	// Execute the command; it should produce tea.Quit.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestErrorBarSetAndClear(t *testing.T) {
	m := New(Managers{})

	// Size the model so View renders the full layout (tab strip + content + bar).
	out, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = out.(Model)

	// An error sets the banner.
	out, _ = m.Update(errMsg{err: errOf("boom")})
	m = out.(Model)
	if m.err == "" {
		t.Fatal("expected error banner to be set")
	}
	if got := m.View(); !contains(got, "boom") {
		t.Fatalf("View does not render error: %q", got)
	}

	// Any keypress clears the banner (without performing the action).
	out, _ = m.Update(runeKey("j"))
	m = out.(Model)
	if m.err != "" {
		t.Fatal("expected error banner to be cleared after keypress")
	}
}

// errOf returns a simple error (avoids importing "errors" just for tests).
func errOf(s string) error { return &strErr{s} }

type strErr struct{ s string }

func (e *strErr) Error() string { return e.s }

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
