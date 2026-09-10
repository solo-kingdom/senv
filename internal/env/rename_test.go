package env

import (
	"strings"
	"testing"
)

func TestRenameKeyPreservesValue(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Set("default", "OLD_KEY", "s3cret"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := mgr.RenameKey("default", "OLD_KEY", "NEW_KEY"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := mgr.Get("default", "NEW_KEY")
	if err != nil {
		t.Fatalf("get new key: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("value after rename = %q, want s3cret", got)
	}
	if _, err := mgr.Get("default", "OLD_KEY"); err == nil {
		t.Error("old key still readable after rename")
	}
	vars, err := mgr.List("default")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(vars["default"]) != 1 {
		t.Errorf("default group has %d vars, want 1", len(vars["default"]))
	}
}

func TestRenameKeyRejectsConflictsAndMissing(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Set("default", "A", "1"); err != nil {
		t.Fatalf("set A: %v", err)
	}
	if err := mgr.Set("default", "B", "2"); err != nil {
		t.Fatalf("set B: %v", err)
	}
	if err := mgr.RenameKey("default", "A", "B"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflicting rename error = %v, want already-exists", err)
	}
	if got, err := mgr.Get("default", "A"); err != nil || got != "1" {
		t.Fatalf("source changed by failed rename: %q, %v", got, err)
	}
	if err := mgr.RenameKey("default", "MISSING", "C"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing source error = %v, want not-found", err)
	}
	if err := mgr.RenameKey("default", "a/b", "C"); err == nil {
		t.Error("invalid old key accepted")
	}
	if err := mgr.RenameKey("default", "A", "b-c"); err == nil {
		t.Error("invalid new key accepted")
	}
}

func TestRenameGroupKeepsVariablesAndActivation(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.AddGroup("staging"); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if err := mgr.Set("staging", "TOKEN", "abc"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := mgr.ActivateGroup("staging"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	if err := mgr.RenameGroup("staging", "prod"); err != nil {
		t.Fatalf("rename group: %v", err)
	}
	got, err := mgr.Get("prod", "TOKEN")
	if err != nil {
		t.Fatalf("get after rename: %v", err)
	}
	if got != "abc" {
		t.Errorf("value after group rename = %q, want abc", got)
	}
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	var found bool
	for _, g := range groups {
		if g.Name == "prod" {
			found = true
			if !g.IsActive {
				t.Error("renamed group lost its activation state")
			}
		}
		if g.Name == "staging" {
			t.Error("staging group still listed after rename")
		}
	}
	if !found {
		t.Fatal("prod group missing after rename")
	}
}

func TestRenameGroupRefusesDefaultAndConflicts(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.AddGroup("staging"); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if err := mgr.RenameGroup("default", "main"); err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("rename default error = %v, want refusal", err)
	}
	if err := mgr.RenameGroup("staging", "default"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflict error = %v, want already-exists", err)
	}
	if err := mgr.RenameGroup("nope", "other"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing group error = %v", err)
	}
}

func TestDeleteGroup(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.AddGroup("tmp"); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if err := mgr.Set("tmp", "K", "v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := mgr.DeleteGroup("default", false); err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("delete default error = %v, want refusal", err)
	}
	if err := mgr.DeleteGroup("tmp", false); err != nil {
		t.Fatalf("delete inactive group: %v", err)
	}
	if _, err := mgr.Get("tmp", "K"); err == nil {
		t.Error("variable survived group delete")
	}
}

func TestDeleteActiveGroupRequiresConfirmation(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.AddGroup("prod"); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if err := mgr.Set("prod", "K", "v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := mgr.ActivateGroup("prod"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := mgr.DeleteGroup("prod", false); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("delete active group error = %v, want refusal", err)
	}
	if _, err := mgr.Get("prod", "K"); err != nil {
		t.Fatalf("refused delete removed data: %v", err)
	}
	if err := mgr.DeleteGroup("prod", true); err != nil {
		t.Fatalf("delete active group with confirmation: %v", err)
	}
	// The stale active entry must be gone, otherwise Export fails.
	if out, err := mgr.Export(); err != nil {
		t.Fatalf("export after deleting active group: %v", err)
	} else if strings.Contains(out, "K=") {
		t.Errorf("export still contains deleted group data: %q", out)
	}
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	for _, g := range groups {
		if g.Name == "prod" {
			t.Error("deleted group still listed")
		}
	}
}
