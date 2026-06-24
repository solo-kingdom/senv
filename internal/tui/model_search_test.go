package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// flushModel applies a command's message chain to the Model until no command
// remains, returning the final model. It does not run the cursor-blink loop
// because an unrecognized message yields a nil command from the active tab.
func flushModel(m Model, cmd tea.Cmd) Model {
	const max = 16
	for i := 0; i < max; i++ {
		if cmd == nil {
			break
		}
		msg := cmd()
		if msg == nil {
			break
		}
		next, nextCmd := m.Update(msg)
		m = next.(Model)
		cmd = nextCmd
	}
	return m
}

func TestModelSearchOpensOnS(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("default", "FOO", "bar"); err != nil {
		t.Fatalf("env set: %v", err)
	}
	m := New(mgrs)

	// Press "S" (shift+s) to open the global search overlay.
	next, cmd := m.Update(runeKey("S"))
	m = next.(Model)
	if m.search == nil {
		t.Fatal("expected search overlay to be open after pressing S")
	}
	// Run the gather command so the overlay has data.
	m = flushModel(m, cmd)
	if len(m.search.gathered) == 0 {
		t.Fatal("search overlay did not gather any entries")
	}
}

func TestModelSearchJumpSelectsEntry(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("prod", "API_KEY", "x"); err != nil {
		t.Fatalf("env set: %v", err)
	}

	m := New(mgrs)
	// Load the env tab data first so jump can resolve indices.
	m = flushModel(m, m.tabs[0].Init())

	// Simulate a search-result jump into env/prod/API_KEY.
	next, _ := m.Update(searchJumpMsg{resultType: typeEnv, group: "prod", key: "API_KEY"})
	m = next.(Model)

	if m.active != 0 {
		t.Fatalf("active tab = %d, want 0 (Env)", m.active)
	}
	if m.search != nil {
		t.Error("search overlay should be closed after jump")
	}
	et := m.tabs[0].(*envTab)
	if et.currentGroup() != "prod" {
		t.Fatalf("group = %q, want prod", et.currentGroup())
	}
	it, ok := et.currentItem()
	if !ok || it.key != "API_KEY" {
		t.Fatalf("item = %+v, want API_KEY", it)
	}
}
