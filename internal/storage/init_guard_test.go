package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitGuard_RefusesWhenOrphanedEnvExists covers the desync-prevention case:
// data dir holds encrypted files but no metadata => Initialize must refuse and
// must NOT create a new metadata.json.
func TestInitGuard_RefusesWhenOrphanedEnvExists(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	// Plant an orphaned env file.
	if err := os.WriteFile(filepath.Join(data, "env_default.json.enc"), []byte("ciphertext"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	mgr := NewManager(cfg, data)
	err := mgr.Initialize("any-password")
	if !errors.Is(err, ErrOrphanedData) {
		t.Fatalf("expected ErrOrphanedData, got %v", err)
	}

	// Must not have created metadata.json.
	if _, statErr := os.Stat(filepath.Join(cfg, MetadataFile)); statErr == nil {
		t.Fatal("Initialize must not create metadata when refusing due to orphaned data")
	}
}

func TestInitGuard_RefusesWhenOrphanedTextExists(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	textGroup := filepath.Join(data, TextDirName, "notes")
	if err := os.MkdirAll(textGroup, 0o700); err != nil {
		t.Fatalf("mkdir texts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(textGroup, "README"+TextFileSuffix), []byte("x"), 0o600); err != nil {
		t.Fatalf("write text: %v", err)
	}

	mgr := NewManager(cfg, data)
	if err := mgr.Initialize("any-password"); !errors.Is(err, ErrOrphanedData) {
		t.Fatalf("expected ErrOrphanedData for orphaned text, got %v", err)
	}
}

func TestInitGuard_EmptyDirsInitializeNormally(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	mgr := NewManager(cfg, data)
	if err := mgr.Initialize("test-password"); err != nil {
		t.Fatalf("Initialize on empty dirs should succeed, got %v", err)
	}
	if !mgr.IsInitialized() {
		t.Fatal("project should be initialized after successful Initialize")
	}
}

func TestInitGuard_AlreadyInitializedStillReportsAlready(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	mgr := NewManager(cfg, data)
	if err := mgr.Initialize("test-password"); err != nil {
		t.Fatalf("first Initialize: %v", err)
	}

	err := mgr.Initialize("test-password")
	if err == nil {
		t.Fatal("expected error on second Initialize")
	}
	// Must report already-initialized, NOT orphaned data (metadata exists).
	if errors.Is(err, ErrOrphanedData) {
		t.Fatalf("must not report orphan when metadata exists: %v", err)
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Fatalf("expected already-initialized error, got %v", err)
	}
}

func TestHasOrphanedData_FalseOnFreshDirs(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManager(filepath.Join(tmp, "cfg"), filepath.Join(tmp, "data"))
	if mgr.HasOrphanedData() {
		t.Fatal("fresh project must not report orphaned data")
	}
}
