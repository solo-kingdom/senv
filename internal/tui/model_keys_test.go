package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// newFullManagers registers every optional tab (SSH / AI / MCP / History /
// Audit) so number-key and overlay tests exercise the 8-tab layout.
func newFullManagers(t *testing.T) Managers {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	home := t.TempDir()
	return Managers{
		Env:       env.NewManager(sm, "pw"),
		Text:      text.NewManager(sm, "pw"),
		Config:    config.NewManager(sm, "pw"),
		SSH:       ssh.NewManager(sm, "pw"),
		LLM:       llm.NewProviderManager(sm, "pw"),
		MCP:       mcp.NewManager(sm, "pw"),
		MCPHome:   home,
		MCPLedger: filepath.Join(home, "mcp-exports.json"),
		History:   &fakeHistorySource{},
		Audit:     &fakeAuditSource{},
	}
}

func TestNumberKeyReachesEveryRegisteredTab(t *testing.T) {
	m := New(newFullManagers(t))
	if len(m.tabs) != 9 {
		t.Fatalf("tabs = %d, want 9", len(m.tabs))
	}
	for i := 1; i <= 9; i++ {
		key := string(rune('0' + i))
		out, _ := m.Update(runeKey(key))
		m = out.(Model)
		if m.active != i-1 {
			t.Fatalf("after pressing %q: active = %d, want %d", key, m.active, i-1)
		}
	}
}

func TestNumberKeyOutOfRangeIgnored(t *testing.T) {
	m := New(Managers{}) // three tabs only (git-like integration)
	if len(m.tabs) != 3 {
		t.Fatalf("tabs = %d, want 3", len(m.tabs))
	}
	out, _ := m.Update(runeKey("2"))
	m = out.(Model)
	for _, key := range []string{"4", "5", "9"} {
		out, _ = m.Update(runeKey(key))
		m = out.(Model)
		if m.active != 1 {
			t.Fatalf("pressing %q moved the cursor to tab %d; want it ignored", key, m.active)
		}
	}
}

func TestHelpOverlayOpensRendersAndCloses(t *testing.T) {
	m := New(newFullManagers(t))
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)

	out, _ = m.Update(runeKey("?"))
	m = out.(Model)
	if m.help == nil {
		t.Fatal("expected help overlay to be open after pressing ?")
	}
	view := m.View()
	for _, want := range []string{"Global", "jump to tab", "keybinding overview", "Env"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help overlay missing %q:\n%s", want, view)
		}
	}

	// `esc` closes it and the tab view comes back.
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if cmd != nil {
		m = flushModel(m, cmd)
	}
	if m.help != nil {
		t.Fatal("expected help overlay to close on esc")
	}
}

func TestHelpOverlayNotTriggeredInInputMode(t *testing.T) {
	m := New(newFullManagers(t))
	// "/" puts the Env tab into filter (text input) mode.
	out, _ := m.Update(runeKey("/"))
	m = out.(Model)
	if !m.tabs[m.active].InputMode() {
		t.Fatal("expected Env tab to be in input mode after /")
	}
	out, _ = m.Update(runeKey("?"))
	m = out.(Model)
	if m.help != nil {
		t.Fatal("help overlay must not open while a tab captures text input")
	}
}

func TestSearchOverlayRendersInContentArea(t *testing.T) {
	mgrs := newFullManagers(t)
	if err := mgrs.Env.Set("default", "FOO", "bar"); err != nil {
		t.Fatalf("env set: %v", err)
	}
	m := New(mgrs)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)

	next, cmd := m.Update(runeKey("S"))
	m = next.(Model)
	m = flushModel(m, cmd)
	if m.search == nil {
		t.Fatal("expected search overlay to be open")
	}
	if view := m.View(); !strings.Contains(view, "Search") {
		t.Fatalf("search overlay is not rendered in the content area:\n%s", view)
	}
}

// flushTab drives a tab's command/message loop until it settles, expanding
// batched commands. It is capped so self-perpetuating commands (the text input
// blink loop) cannot hang a test.
func flushTab(tab Tab, cmd tea.Cmd) Tab {
	queue := make([]tea.Cmd, 0, 4)
	if cmd != nil {
		queue = append(queue, cmd)
	}
	for steps := 0; len(queue) > 0 && steps < 64; steps++ {
		c := queue[0]
		queue = queue[1:]
		msg := c()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		next, nextCmd := tab.Update(msg)
		tab = next
		if nextCmd != nil {
			queue = append(queue, nextCmd)
		}
	}
	return tab
}

// runCmd collects every message a command produces, descending into batches.
// Tests use it to observe toast messages that are batched with a reload.
func runCmd(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	if cmd == nil {
		return out
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	if msg != nil {
		out = append(out, msg)
	}
	return out
}
