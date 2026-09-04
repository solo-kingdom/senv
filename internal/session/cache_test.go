package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setRuntimeProbe(t *testing.T, kind runtimeFilesystemKind, probeErr error) {
	t.Helper()
	original := runtimeFilesystemProbe
	runtimeFilesystemProbe = func(string) (runtimeFilesystemKind, error) { return kind, probeErr }
	t.Cleanup(func() { runtimeFilesystemProbe = original })
}

func startSessionForCacheTest(t *testing.T, timeoutValue string) (string, string, *Manager) {
	t.Helper()
	cfg, data := setupProject(t, "correct-secret")
	timeout, err := ParseTimeout(timeoutValue)
	if err != nil {
		t.Fatalf("ParseTimeout(%q): %v", timeoutValue, err)
	}
	manager := sessionManagerForTest(t, cfg, data)
	if err := manager.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("StartSession(%q): %v", timeoutValue, err)
	}
	return cfg, data, manager
}

func TestSessionCacheFilesystemRejectsDiskBackedXDG(t *testing.T) {
	isolateSessionCache(t)
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	setRuntimeProbe(t, runtimeFilesystemDisk, nil)
	cfg, data := setupProject(t, "correct-secret")
	timeout, _ := ParseTimeout("never")

	err := sessionManagerForTest(t, cfg, data).StartSession("correct-secret", timeout)
	if !errors.Is(err, errUnsafeRuntimeFilesystem) {
		t.Fatalf("StartSession error = %v, want unsafe filesystem", err)
	}
	if entries, err := os.ReadDir(runtimeDir); err != nil || len(entries) != 0 {
		t.Fatalf("unsafe XDG runtime changed: entries=%v err=%v", entries, err)
	}
}

func TestSessionCacheFilesystemRejectsDiskBackedFallback(t *testing.T) {
	isolateSessionCache(t)
	fallbackRoot := t.TempDir()
	t.Setenv("TMPDIR", fallbackRoot)
	t.Setenv("XDG_RUNTIME_DIR", "")
	setRuntimeProbe(t, runtimeFilesystemDisk, nil)
	cfg, data := setupProject(t, "correct-secret")
	timeout, _ := ParseTimeout("never")

	err := sessionManagerForTest(t, cfg, data).StartSession("correct-secret", timeout)
	if !errors.Is(err, errUnsafeRuntimeFilesystem) {
		t.Fatalf("StartSession error = %v, want unsafe filesystem", err)
	}
	if entries, err := os.ReadDir(fallbackRoot); err != nil || len(entries) != 0 {
		t.Fatalf("unsafe fallback changed: entries=%v err=%v", entries, err)
	}
}

func TestSessionCacheFallbackIsRandomAndPrivate(t *testing.T) {
	isolateSessionCache(t)
	fallbackRoot := t.TempDir()
	t.Setenv("TMPDIR", fallbackRoot)
	t.Setenv("XDG_RUNTIME_DIR", "")
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)
	_, _, manager := startSessionForCacheTest(t, "never")

	entries, err := os.ReadDir(fallbackRoot)
	if err != nil {
		t.Fatalf("read fallback root: %v", err)
	}
	var cacheDir os.DirEntry
	for _, entry := range entries {
		if entry.Name() == fallbackLifecycleLockName() {
			continue
		}
		if cacheDir != nil {
			t.Fatalf("fallback entries=%v, want one cache directory and lock", entries)
		}
		cacheDir = entry
	}
	if cacheDir == nil {
		t.Fatal("fallback cache directory is missing")
	}
	name := cacheDir.Name()
	if !strings.HasPrefix(name, fallbackDirPrefix()) || name == fmt.Sprintf("senv-%d", os.Getuid()) {
		t.Fatalf("fallback directory %q is not randomized", name)
	}
	info, err := cacheDir.Info()
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("fallback mode=%v err=%v, want 0700", info.Mode(), err)
	}
	cacheInfo, err := os.Stat(filepath.Join(fallbackRoot, name, cacheFileName()))
	if err != nil || cacheInfo.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode=%v err=%v, want 0600", cacheInfo.Mode(), err)
	}
	if _, err := manager.GetCachedKey(); err != nil {
		t.Fatalf("fallback cache is not discoverable: %v", err)
	}
}

func TestSessionCacheFilesystemSupportsAllTimeoutModes(t *testing.T) {
	for _, timeoutValue := range []string{"never", "restart", "5m"} {
		t.Run(timeoutValue, func(t *testing.T) {
			isolateSessionCache(t)
			setRuntimeProbe(t, runtimeFilesystemMemory, nil)
			_, _, manager := startSessionForCacheTest(t, timeoutValue)
			cache, err := manager.LoadCache()
			if err != nil || cache == nil {
				t.Fatalf("LoadCache: cache=%v err=%v", cache, err)
			}
			if cache.SessionID == "" || cache.Key == "" {
				t.Fatal("cache omitted session ID or key")
			}
		})
	}
}

func TestSessionCacheSymlinkTargetRejected(t *testing.T) {
	isolateSessionCache(t)
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if err := os.Mkdir(filepath.Join(runtimeDir, "senv"), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(runtimeDir, "senv", cacheFileName())); err != nil {
		t.Fatal(err)
	}
	cfg, data := setupProject(t, "correct-secret")
	timeout, _ := ParseTimeout("never")
	if err := sessionManagerForTest(t, cfg, data).StartSession("correct-secret", timeout); err == nil {
		t.Fatal("cache symlink was accepted")
	}
	if got, _ := os.ReadFile(sentinel); string(got) != "unchanged" {
		t.Fatalf("symlink target changed to %q", got)
	}
}

func TestSessionCacheSymlinkParentRejected(t *testing.T) {
	isolateSessionCache(t)
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)
	realRuntime := t.TempDir()
	parent := t.TempDir()
	linkedRuntime := filepath.Join(parent, "runtime")
	if err := os.Symlink(realRuntime, linkedRuntime); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", linkedRuntime)
	cfg, data := setupProject(t, "correct-secret")
	timeout, _ := ParseTimeout("never")
	if err := sessionManagerForTest(t, cfg, data).StartSession("correct-secret", timeout); err == nil {
		t.Fatal("runtime parent symlink was accepted")
	}
	if entries, _ := os.ReadDir(realRuntime); len(entries) != 0 {
		t.Fatalf("symlink target runtime changed: %v", entries)
	}
}

func TestSessionCacheRandomSessionIDFailureLeavesNoCache(t *testing.T) {
	isolateSessionCache(t)
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)
	original := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("random unavailable") }
	t.Cleanup(func() { randRead = original })
	cfg, data := setupProject(t, "correct-secret")
	timeout, _ := ParseTimeout("never")
	if err := sessionManagerForTest(t, cfg, data).StartSession("correct-secret", timeout); err == nil {
		t.Fatal("session ID random failure was ignored")
	}
	if entries, _ := os.ReadDir(os.Getenv("XDG_RUNTIME_DIR")); len(entries) != 0 {
		t.Fatalf("random failure wrote runtime entries: %v", entries)
	}
}

func TestSessionCacheRandomFallbackFailureLeavesNoCache(t *testing.T) {
	isolateSessionCache(t)
	fallbackRoot := t.TempDir()
	t.Setenv("TMPDIR", fallbackRoot)
	t.Setenv("XDG_RUNTIME_DIR", "")
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)
	original := randRead
	calls := 0
	randRead = func(value []byte) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("random unavailable")
		}
		for i := range value {
			value[i] = byte(i + 1)
		}
		return len(value), nil
	}
	t.Cleanup(func() { randRead = original })
	cfg, data := setupProject(t, "correct-secret")
	timeout, _ := ParseTimeout("never")
	if err := sessionManagerForTest(t, cfg, data).StartSession("correct-secret", timeout); err == nil {
		t.Fatal("fallback random failure was ignored")
	}
	if entries, _ := os.ReadDir(fallbackRoot); len(entries) != 1 || entries[0].Name() != fallbackLifecycleLockName() {
		t.Fatalf("fallback random failure wrote cache entries: %v", entries)
	}
}

func TestSessionCacheFilesystemClearsLegacyPersistentCache(t *testing.T) {
	isolateSessionCache(t)
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)
	legacy := legacyPersistentCachePath()
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"key":"legacy"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	startSessionForCacheTest(t, "never")
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy persistent cache still exists: %v", err)
	}
}
