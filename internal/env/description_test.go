package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestSetRejectsMissingGroup(t *testing.T) {
	mgr := newTestManager(t)
	err := mgr.Set("newsvc", "FOO", "bar")
	if err == nil {
		t.Fatal("Set into missing group should fail")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %v, want missing-group message", err)
	}
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Name == "newsvc" {
			t.Fatal("missing group was created")
		}
	}
}

func TestAddGroupRequiresDescription(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.AddGroup("svc", "   "); err == nil {
		t.Fatal("blank description should fail")
	}
	if err := mgr.AddGroup("svc", "archive of self-use keys"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range groups {
		if g.Name == "svc" {
			found = true
			if g.Description != "archive of self-use keys" {
				t.Fatalf("description = %q", g.Description)
			}
		}
	}
	if !found {
		t.Fatal("group not listed")
	}
}

func TestSetWithDescriptionKeepsOnOmit(t *testing.T) {
	mgr := newTestManager(t)
	desc := "oss key"
	if err := mgr.SetWithDescription("default", "OSS_KEY", "v1", &desc); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("default", "OSS_KEY", "v2"); err != nil {
		t.Fatal(err)
	}
	_, got, err := mgr.GetWithMeta("default", "OSS_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if got != "oss key" {
		t.Fatalf("description = %q, want kept", got)
	}
}

func TestKeyCollisionWarnings(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Set("default", "OSS_ACCESS_KEY_ID", "personal"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddGroup("feg", "work variant"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("feg", "OSS_ACCESS_KEY_ID", "work"); err != nil {
		t.Fatal(err)
	}
	none, err := mgr.KeyCollisionWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("inactive group should not collide: %v", none)
	}
	if err := mgr.ActivateGroup("feg"); err != nil {
		t.Fatal(err)
	}
	warnings, err := mgr.KeyCollisionWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "OSS_ACCESS_KEY_ID") || !strings.Contains(warnings[0], "feg") {
		t.Fatalf("warnings = %v", warnings)
	}
	if err := mgr.Set("feg", "UNIQUE_KEY", "x"); err != nil {
		t.Fatal(err)
	}
	warnings, err = mgr.KeyCollisionWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("unique key should not add a warning: %v", warnings)
	}
}

func TestGetWithMetaDoesNotMaskDecryptError(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Set("default", "FOO", "secret"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(mgr.storage.GetDataPath(), storage.EnvDirName, "default", "FOO"+storage.EnvVarSuffix)
	if err := os.WriteFile(path, []byte("not-ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := mgr.GetWithMeta("default", "FOO")
	if err == nil {
		t.Fatal("corrupt ciphertext should fail")
	}
	if strings.Contains(err.Error(), "not found") {
		t.Fatalf("decrypt failure was swallowed as missing: %v", err)
	}
	if _, err := mgr.Get("default", "FOO"); err == nil || strings.Contains(err.Error(), "not found") {
		t.Fatalf("Get should surface decrypt failure, got %v", err)
	}
}

func TestGetReadsLegacyGroup(t *testing.T) {
	mgr := newTestManager(t)
	writeLegacyEnvGroup(t, mgr, "archive", "self-use keys", "TOKEN", "legacy-secret")

	got, desc, err := mgr.GetWithMeta("archive", "TOKEN")
	if err != nil {
		t.Fatalf("GetWithMeta legacy: %v", err)
	}
	if got != "legacy-secret" {
		t.Fatalf("value = %q", got)
	}
	if desc != "" {
		t.Fatalf("per-var description = %q, want empty", desc)
	}
	value, err := mgr.Get("archive", "TOKEN")
	if err != nil {
		t.Fatalf("Get legacy: %v", err)
	}
	if value != "legacy-secret" {
		t.Fatalf("Get value = %q", value)
	}
}

func TestListGroupsReadsLegacyDescription(t *testing.T) {
	mgr := newTestManager(t)
	writeLegacyEnvGroup(t, mgr, "archive", "self-use keys", "TOKEN", "x")
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Name == "archive" {
			if g.Description != "self-use keys" {
				t.Fatalf("description = %q", g.Description)
			}
			return
		}
	}
	t.Fatal("legacy group not listed")
}
