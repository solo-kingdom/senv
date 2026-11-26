package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestWelcomePageDefaultAndQuickJump(t *testing.T) {
	m := New(newAll14Managers(t))
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = out.(Model)

	if !m.onWelcome {
		t.Fatal("expected New() to start on Welcome page")
	}
	view := m.View()
	assertFrameInvariants(t, "welcome 120x30", view, 120, 30)
	plain := ansi.Strip(view)
	for _, want := range []string{"╭ Senv ", "Welcome to Senv", "Workspaces (14)", "Common Shortcuts", "Navigation", "In-Tab Actions", "Config", "History", "Audit", "Backup", "Settings"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("welcome view missing %q:\n%s", want, plain)
		}
	}

	// Move cursor to the last workspace (Settings) with G and press Enter to jump.
	out, _ = m.Update(runeKey("G"))
	m = out.(Model)
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil {
		m = flushModel(m, cmd)
	}
	if m.onWelcome {
		t.Fatal("expected Enter on Welcome workspace to leave Welcome page")
	}
	if got := m.tabs[m.active].Title(); got != "Settings" {
		t.Fatalf("active tab = %q, want Settings", got)
	}

	// Press 0 to return to Home / Welcome page from any tab.
	out, _ = m.Update(runeKey("0"))
	m = out.(Model)
	if !m.onWelcome {
		t.Fatal("expected 0 to return to Home / Welcome page")
	}
}

func TestWelcomePageFrameInvariantsAcrossSizes(t *testing.T) {
	m := New(newAll14Managers(t))
	for _, sz := range [][2]int{{30, 7}, {40, 12}, {60, 20}, {80, 24}, {120, 30}, {160, 40}} {
		out, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		m = out.(Model)
		assertFrameInvariants(t, "welcome", m.View(), sz[0], sz[1])
	}
}
