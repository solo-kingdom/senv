package text

import (
	"strings"
	"testing"
)

func TestRenameKeyPreservesContent(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.Set("default", "old", "hello"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := mgr.RenameKey("default", "old", "new"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := mgr.Get("default", "new")
	if err != nil {
		t.Fatalf("get new: %v", err)
	}
	if got != "hello" {
		t.Errorf("content after rename = %q, want hello", got)
	}
	if _, err := mgr.Get("default", "old"); err == nil {
		t.Error("old key still readable after rename")
	}
}

func TestRenameKeyRejectsConflictsAndMissing(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.Set("default", "a", "1"); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := mgr.Set("default", "b", "2"); err != nil {
		t.Fatalf("set b: %v", err)
	}
	if err := mgr.RenameKey("default", "a", "b"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflict error = %v, want already-exists", err)
	}
	if err := mgr.RenameKey("default", "missing", "c"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing source error = %v, want not-found", err)
	}
	if err := mgr.RenameKey("default", "a/b", "c"); err == nil {
		t.Error("invalid old key accepted")
	}
}

func TestRenameGroupKeepsBlocks(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.AddGroup("notes"); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if err := mgr.Set("notes", "todo", "buy milk"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := mgr.RenameGroup("notes", "journal"); err != nil {
		t.Fatalf("rename group: %v", err)
	}
	got, err := mgr.Get("journal", "todo")
	if err != nil {
		t.Fatalf("get after group rename: %v", err)
	}
	if got != "buy milk" {
		t.Errorf("content after group rename = %q", got)
	}
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	for _, g := range groups {
		if g.Name == "notes" {
			t.Error("old group name still listed")
		}
	}
	if err := mgr.RenameGroup("journal", "notes"); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	if err := mgr.RenameGroup("journal", "other"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing group error = %v, want does-not-exist", err)
	}
	if err := mgr.AddGroup("existing"); err != nil {
		t.Fatalf("add conflicting group: %v", err)
	}
	if err := mgr.RenameGroup("notes", "existing"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflict error = %v, want already-exists", err)
	}
}
