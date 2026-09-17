package text

import (
	"strings"
	"testing"
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
