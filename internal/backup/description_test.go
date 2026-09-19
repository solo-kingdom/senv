package backup

import (
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestSetRejectsMissingGroup(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	err := mgr.Set("newnotes", "KEY", "val")
	if err == nil {
		t.Fatal("Set into missing group should fail")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %v", err)
	}
}

func TestAddGroupRequiresDescription(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.AddGroup("secrets", ""); err == nil {
		t.Fatal("empty description should fail")
	}
	if err := mgr.AddGroup("secrets", "credential archive"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
}

func TestSetWithDescriptionPersists(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	note := "weekly draft"
	if err := mgr.SetWithDescription("default", "README", "body", &note); err != nil {
		t.Fatal(err)
	}
	infos, err := mgr.List("default")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, info := range infos {
		if info.Key == "README" {
			found = true
			if info.Description != "weekly draft" {
				t.Fatalf("description = %q", info.Description)
			}
		}
	}
	if !found {
		t.Fatal("README not listed")
	}
	if err := mgr.Set("default", "README", "body2"); err != nil {
		t.Fatal(err)
	}
	_, desc, err := mgr.GetWithMeta("default", "README")
	if err != nil {
		t.Fatal(err)
	}
	if desc != "weekly draft" {
		t.Fatalf("omitted description was overwritten: %q", desc)
	}
}

func TestSetDescriptionTooLong(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	tooLong := strings.Repeat("a", storage.MaxDescriptionBytes+1)
	if err := mgr.SetWithDescription("default", "DUMP", "body", &tooLong); err == nil {
		t.Fatal("description over 2048 bytes should fail")
	}
	if _, err := mgr.Get("default", "DUMP"); err == nil {
		t.Fatal("oversized description must not create the entry")
	}
}

func TestEnsureGroupIsIdempotent(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.EnsureGroup("notes", "draft archive"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnsureGroup("notes", "other description"); err != nil {
		t.Fatalf("second EnsureGroup: %v", err)
	}
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range groups {
		if g.Name == "notes" {
			found = true
			if g.Description != "draft archive" {
				t.Fatalf("description overwritten = %q", g.Description)
			}
		}
	}
	if !found {
		t.Fatal("notes group missing")
	}
}
