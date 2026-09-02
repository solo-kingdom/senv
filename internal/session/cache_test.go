package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNeverSessionStaysOnTmpfs(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	to, err := ParseTimeout("never")
	if err != nil || to == nil {
		t.Fatalf("parse timeout: %v", err)
	}

	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("start never session: %v", err)
	}

	// No cache file may exist under the persistent user-data location.
	legacy := filepath.Join(os.Getenv("HOME"), ".local", "share", "senv", "session", cacheFileName())
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("never session must not persist to %s (stat err: %v)", legacy, err)
	}

	// The cache must live under XDG_RUNTIME_DIR with 0600 permissions.
	runtimeCache := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "senv", cacheFileName())
	fi, err := os.Stat(runtimeCache)
	if err != nil {
		t.Fatalf("expected cache at %s: %v", runtimeCache, err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("cache perm = %o, want 600", got)
	}
}

func TestLegacyPersistentCacheCleanedOnStart(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")

	// Plant a cache file where older versions stored "never" sessions.
	legacyDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "senv", "session")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	legacy := filepath.Join(legacyDir, cacheFileName())
	if err := os.WriteFile(legacy, []byte(`{"key":"stale"}`), 0o600); err != nil {
		t.Fatalf("plant legacy cache: %v", err)
	}

	to, _ := ParseTimeout("never")
	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("start session: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy persistent cache must be removed on session start (stat err: %v)", err)
	}
}

func TestTmpFallbackPrivateDir(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")

	// Empty XDG_RUNTIME_DIR sends the cache to the /tmp fallback.
	t.Setenv("XDG_RUNTIME_DIR", "")
	fallbackDir := filepath.Join(os.TempDir(), fmt.Sprintf("senv-%d", os.Getuid()))
	os.RemoveAll(fallbackDir)
	t.Cleanup(func() { os.RemoveAll(fallbackDir) })

	to, _ := ParseTimeout("never")
	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("start session with /tmp fallback: %v", err)
	}

	// Random uid dir: private 0700, cache file 0600.
	dirInfo, err := os.Stat(fallbackDir)
	if err != nil {
		t.Fatalf("stat fallback dir: %v", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("fallback dir mode = %v, want drwx------", dirInfo.Mode())
	}
	fi, err := os.Stat(filepath.Join(fallbackDir, cacheFileName()))
	if err != nil {
		t.Fatalf("stat cache file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("cache perm = %o, want 600", fi.Mode().Perm())
	}

	// Restarting the session must succeed (remove + exclusive re-create).
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("restarting session must replace the cache: %v", err)
	}
}

func TestTmpFallbackRejectsTamperedDir(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")

	t.Setenv("XDG_RUNTIME_DIR", "")
	fallbackDir := filepath.Join(os.TempDir(), fmt.Sprintf("senv-%d", os.Getuid()))
	os.RemoveAll(fallbackDir)
	t.Cleanup(func() { os.RemoveAll(fallbackDir) })

	// A loose directory (e.g. planted by another local user or a symlink
	// target) must not be accepted as cache location.
	if err := os.MkdirAll(fallbackDir, 0o755); err != nil {
		t.Fatalf("plant loose dir: %v", err)
	}

	to, _ := ParseTimeout("never")
	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err == nil {
		t.Fatal("session start must fail when the fallback dir is not private")
	}
	if _, err := os.Stat(filepath.Join(fallbackDir, cacheFileName())); !os.IsNotExist(err) {
		t.Error("no cache file may be written into a tampered fallback dir")
	}
}

func TestGenerateSessionIDFailsClosedOnRandError(t *testing.T) {
	orig := randRead
	randRead = func(b []byte) (int, error) { return 0, errors.New("boom") }
	t.Cleanup(func() { randRead = orig })

	if _, err := generateSessionID(); err == nil {
		t.Fatal("expected error when randomness is unavailable")
	}
}
