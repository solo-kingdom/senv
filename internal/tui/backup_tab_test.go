package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/storage"
)

func newTestBackupManager(t *testing.T) *backup.Manager {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(dir+"/cfg", dir+"/data")
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return backup.NewManager(sm, "pw")
}

func flushBackup(tab *backupTab, cmd tea.Cmd) *backupTab {
	for cmd != nil {
		msg := cmd()
		next, nextCmd := tab.Update(msg)
		tab = next.(*backupTab)
		cmd = nextCmd
	}
	return tab
}

func TestBackupListOmitsValueAndKeepsDescription(t *testing.T) {
	mgr := newTestBackupManager(t)
	note := "weekly dump"
	if err := mgr.SetWithDescription("default", "DUMP", "SECRET-BACKUP-BODY", &note); err != nil {
		t.Fatal(err)
	}
	tab := newBackupTab(Managers{Backup: mgr})
	tab.SetSize(120, 20)
	tab = flushBackup(tab, tab.load())
	for i, g := range tab.groups {
		if g.name == "default" {
			tab.groupIndex = i
			break
		}
	}
	found := false
	for _, it := range tab.itemsByGroup["default"] {
		if it.key == "DUMP" {
			found = true
			if it.description != note {
				t.Fatalf("description = %q", it.description)
			}
		}
	}
	if !found {
		t.Fatal("DUMP missing from items")
	}
	view := tab.View()
	if strings.Contains(view, "SECRET-BACKUP-BODY") {
		t.Fatalf("list leaked value:\n%s", view)
	}
}

func TestBackupDefaultGroupCannotRenameOrDelete(t *testing.T) {
	tab := newBackupTab(Managers{Backup: newTestBackupManager(t)})
	tab.SetSize(80, 20)
	tab = flushBackup(tab, tab.load())
	for i, g := range tab.groups {
		if g.name == "default" {
			tab.groupIndex = i
			break
		}
	}
	tab.focusLeft = true
	out, cmd := tab.Update(runeKey("r"))
	tab = out.(*backupTab)
	if tab.form != nil {
		t.Fatal("default group must not open rename form")
	}
	if cmd == nil {
		t.Fatal("expected rename warning toast")
	}
	if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "cannot be renamed") {
		t.Fatalf("rename toast = %#v", cmd())
	}
	out, cmd = tab.Update(runeKey("d"))
	tab = out.(*backupTab)
	if tab.mode == backupModeDeleteGroupConfirm {
		t.Fatal("default group must not enter delete confirm")
	}
	if cmd == nil {
		t.Fatal("expected delete warning toast")
	}
	if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "cannot be deleted") {
		t.Fatalf("delete toast = %#v", cmd())
	}
}
