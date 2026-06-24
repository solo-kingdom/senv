package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func newTestTextManager(t *testing.T) *text.Manager {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return text.NewManager(sm, "pw")
}

func hasTextItem(t *textTab, group, key string) bool {
	for _, it := range t.itemsByGroup[group] {
		if it.key == key {
			return true
		}
	}
	return false
}

func hasTextGroup(t *textTab, name string) bool {
	for _, g := range t.groups {
		if g.name == name {
			return true
		}
	}
	return false
}

// flushText mirrors flush but for the text tab type.
func flushText(t *textTab, cmd tea.Cmd) *textTab {
	const max = 16
	for i := 0; i < max; i++ {
		if cmd == nil {
			break
		}
		msg := cmd()
		if msg == nil {
			break
		}
		next, nextCmd := t.Update(msg)
		t = next.(*textTab)
		cmd = nextCmd
	}
	return t
}

func TestTextManagerLoadAndOps(t *testing.T) {
	mgr := newTestTextManager(t)
	if err := mgr.Set("default", "readme", "hello world"); err != nil {
		t.Fatalf("set readme: %v", err)
	}
	if err := mgr.Set("default", "config", "k=v"); err != nil {
		t.Fatalf("set config: %v", err)
	}

	tab := newTextTab(Managers{Text: mgr})
	tab.SetSize(80, 20)
	tab = flushText(tab, tab.load())

	if len(tab.groups) == 0 || tab.groups[0].name != "default" {
		t.Fatalf("expected default group, got %#v", tab.groups)
	}
	items := tab.itemsByGroup["default"]
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !hasTextItem(tab, "default", "readme") || !hasTextItem(tab, "default", "config") {
		t.Errorf("items missing: %#v", items)
	}
	// readme content is 11 bytes; size should reflect it.
	for _, it := range items {
		if it.key == "readme" && it.size != 11 {
			t.Errorf("readme size = %d, want 11", it.size)
		}
	}

	// Add a group, then seed it with a key so it shows up in the listing
	// (empty non-default groups are hidden by the view layer).
	tab = flushText(tab, tab.doAddGroup("prod"))
	if err := mgr.Set("prod", "seed", "v"); err != nil {
		t.Fatalf("seed prod: %v", err)
	}
	tab = flushText(tab, tab.load())
	if !hasTextGroup(tab, "prod") {
		t.Fatal("prod group not visible after seeding a key")
	}

	// Delete a block.
	tab = flushText(tab, tab.doDelete("default", "readme"))
	if hasTextItem(tab, "default", "readme") {
		t.Error("readme should be deleted")
	}

	// Export a block to a file.
	out := filepath.Join(t.TempDir(), "out.txt")
	tab = flushText(tab, tab.doExport("default", "config", out))
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if string(data) != "k=v" {
		t.Errorf("exported = %q, want k=v", string(data))
	}
}

// TestTextEditBuildsExecCommand verifies editKey builds a tea.ExecProcess
// command for the configured editor (the actual vim run needs a TTY and is
// covered by manual verification; Prepare/Finish are covered in text pkg tests).
func TestTextEditBuildsExecCommand(t *testing.T) {
	mgr := newTestTextManager(t)
	mgr.Set("default", "note", "x")

	tab := newTextTab(Managers{Text: mgr})
	tab.SetSize(80, 20)
	tab = flushText(tab, tab.load())

	_, cmd := tab.editCurrent()
	if cmd == nil {
		t.Fatal("editCurrent should return a non-nil exec command")
	}
}
