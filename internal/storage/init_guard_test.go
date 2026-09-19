package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/securefs"
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

func TestInitGuard_RefusesWhenOrphanedBackupExists(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	backupGroup := filepath.Join(data, BackupDirName, "notes")
	if err := os.MkdirAll(backupGroup, 0o700); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupGroup, "DUMP"+BackupFileSuffix), []byte("x"), 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	mgr := NewManager(cfg, data)
	if err := mgr.Initialize("any-password"); !errors.Is(err, ErrOrphanedData) {
		t.Fatalf("expected ErrOrphanedData for orphaned backup, got %v", err)
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

// TestInitGuard_MachineLocalArtifactsDoNotBlockInit 验证仅含机器本地工件
// （TUI 快照、同步 state、锁）的目录不会被误判为 orphan，init 正常完成。
func TestInitGuard_MachineLocalArtifactsDoNotBlockInit(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	for _, name := range []string{"tui-snapshot.enc", ".senv-sync-state.json", ".senv-sync.lock"} {
		if err := os.WriteFile(filepath.Join(data, name), []byte("local"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	mgr := NewManager(cfg, data)
	if mgr.HasOrphanedData() {
		t.Fatal("machine-local artifacts must not count as orphaned user data")
	}
	if err := mgr.Initialize("test-password"); err != nil {
		t.Fatalf("Initialize with only machine-local artifacts must succeed: %v", err)
	}
}

func TestSeedTextGroupDoesNotOverwriteDescription(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	custom := &EnvGroupMeta{
		Name:        "llm-keys",
		Description: "user-edited reserved note",
		CreatedAt:   time.Now(),
	}
	if err := mgr.SaveTextGroupMetaWithKey("llm-keys", custom, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.seedTextGroup("llm-keys", reservedLLMKeysDescription, key); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.LoadTextGroupMetaWithKey("llm-keys", key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "user-edited reserved note" {
		t.Fatalf("description = %q, want preserved", got.Description)
	}
}

type failAtomicRoot struct {
	securefs.TrustedRoot
	match func(segments []string) bool
}

func (r *failAtomicRoot) AtomicWrite(segments []string, data []byte, mode fs.FileMode) error {
	if r.match != nil && r.match(segments) {
		return errors.New("injected atomic write failure")
	}
	return r.TrustedRoot.AtomicWrite(segments, data, mode)
}

func TestSaveEnvGroupMetaRollsBackNewDirectory(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	original := mgr.openRoot
	mgr.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := original(path)
		if err != nil {
			return nil, err
		}
		return &failAtomicRoot{
			TrustedRoot: root,
			match: func(segments []string) bool {
				return len(segments) == 3 && segments[0] == EnvDirName && segments[1] == "newsvc" && segments[2] == EnvMetaFileName
			},
		}, nil
	}
	t.Cleanup(func() { mgr.openRoot = original })

	err := mgr.SaveEnvGroupMetaWithKey("newsvc", &EnvGroupMeta{Name: "newsvc", CreatedAt: time.Now()}, key)
	if err == nil {
		t.Fatal("want injected write failure")
	}
	if _, statErr := os.Stat(filepath.Join(mgr.dataPath, EnvDirName, "newsvc")); !os.IsNotExist(statErr) {
		t.Fatal("failed meta write left an orphan group directory")
	}
}

func TestSaveTextGroupMetaRollsBackNewDirectory(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	original := mgr.openRoot
	mgr.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := original(path)
		if err != nil {
			return nil, err
		}
		return &failAtomicRoot{
			TrustedRoot: root,
			match: func(segments []string) bool {
				return len(segments) == 3 && segments[0] == TextDirName && segments[1] == "scratch" && segments[2] == EnvMetaFileName
			},
		}, nil
	}
	t.Cleanup(func() { mgr.openRoot = original })

	err := mgr.SaveTextGroupMetaWithKey("scratch", &EnvGroupMeta{Name: "scratch", CreatedAt: time.Now()}, key)
	if err == nil {
		t.Fatal("want injected write failure")
	}
	if _, statErr := os.Stat(filepath.Join(mgr.dataPath, TextDirName, "scratch")); !os.IsNotExist(statErr) {
		t.Fatal("failed meta write left an orphan text group directory")
	}
}

func TestSaveBackupGroupMetaRollsBackNewDirectory(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	original := mgr.openRoot
	mgr.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := original(path)
		if err != nil {
			return nil, err
		}
		return &failAtomicRoot{
			TrustedRoot: root,
			match: func(segments []string) bool {
				return len(segments) == 3 && segments[0] == BackupDirName && segments[1] == "scratch" && segments[2] == EnvMetaFileName
			},
		}, nil
	}
	t.Cleanup(func() { mgr.openRoot = original })

	err := mgr.SaveBackupGroupMetaWithKey("scratch", &EnvGroupMeta{Name: "scratch", CreatedAt: time.Now()}, key)
	if err == nil {
		t.Fatal("want injected write failure")
	}
	if _, statErr := os.Stat(filepath.Join(mgr.dataPath, BackupDirName, "scratch")); !os.IsNotExist(statErr) {
		t.Fatal("failed meta write left an orphan backup group directory")
	}
}

func TestInitializeRollsBackWhenGroupSeedFails(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	mgr := NewManager(cfg, data)

	original := mgr.openRoot
	mgr.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := original(path)
		if err != nil {
			return nil, err
		}
		return &failAtomicRoot{
			TrustedRoot: root,
			match: func(segments []string) bool {
				return len(segments) == 3 && segments[0] == TextDirName && segments[1] == "llm-keys" && segments[2] == EnvMetaFileName
			},
		}, nil
	}

	if err := mgr.Initialize("test-password"); err == nil {
		t.Fatal("initialize should fail when llm-keys meta cannot be written")
	}
	if mgr.IsInitialized() {
		t.Fatal("failed initialize left metadata in place")
	}

	mgr.openRoot = original
	if err := mgr.Initialize("test-password"); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

// TestCheckConsistencyIgnoresMachineLocalArtifacts 验证一致性探针不把
// 机器本地工件计入失败清单。
func TestCheckConsistencyIgnoresMachineLocalArtifacts(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := os.WriteFile(filepath.Join(mgr.dataPath, "tui-snapshot.enc"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if !report.AllOK() {
		t.Fatalf("machine-local artifact must not affect consistency: %+v", report)
	}
}
