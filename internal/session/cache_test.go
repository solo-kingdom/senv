package session

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
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
	timeout, _ := ParseTimeout("restart")

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
	timeout, _ := ParseTimeout("restart")

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
	_, data, manager := startSessionForCacheTest(t, "restart")

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
	cacheInfo, err := os.Stat(filepath.Join(fallbackRoot, name, cacheFileName(vaultSlotFor(data))))
	if err != nil || cacheInfo.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode=%v err=%v, want 0600", cacheInfo.Mode(), err)
	}
	if _, err := manager.GetCachedKey(); err != nil {
		t.Fatalf("fallback cache is not discoverable: %v", err)
	}
}

func TestSessionCacheFilesystemSupportsAllTimeoutModes(t *testing.T) {
	for _, timeoutValue := range []string{"restart", "5m"} {
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
	cfg, data := setupProject(t, "correct-secret")
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(runtimeDir, "senv", cacheFileName(vaultSlotFor(data)))); err != nil {
		t.Fatal(err)
	}
	timeout, _ := ParseTimeout("restart")
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
	timeout, _ := ParseTimeout("restart")
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
	timeout, _ := ParseTimeout("restart")
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
	timeout, _ := ParseTimeout("restart")
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
	startSessionForCacheTest(t, "restart")
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy persistent cache still exists: %v", err)
	}
}

// plantBothStores writes one cache into a fake platform store and one into the
// disk escape hatch for the same slot, so multi-cache selection can be tested
// without depending on tmpfs availability.
func plantBothStores(t *testing.T, slot string, primary, hatch *SessionCache) *fakeSessionStore {
	t.Helper()
	fake := &fakeSessionStore{cache: primary}
	setActiveSessionStore(t, fake)
	if err := (diskCacheStore{}).Save(slot, hatch); err != nil {
		t.Fatalf("save hatch: %v", err)
	}
	return fake
}

func baseCache(slot string, created time.Time) *SessionCache {
	return &SessionCache{
		Key:          base64.StdEncoding.EncodeToString(make([]byte, crypto.KeySize)),
		Salt:         "salt",
		CreatedAt:    created,
		ExpiresAt:    created.Add(8 * time.Hour),
		TimeoutType:  string(TimeoutDuration),
		DataPathHash: slot,
		SessionID:    "sess-" + created.Format("150405.000"),
	}
}

func TestLoadCacheMultipleSelection(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	slot := testSlot
	past := time.Now().Add(-2 * time.Hour)

	t.Run("newer hatch wins and both survive", func(t *testing.T) {
		primary := baseCache(slot, past)
		hatch := baseCache(slot, past.Add(time.Hour))
		plantBothStores(t, slot, primary, hatch)

		got, err := loadCache(slot)
		if err != nil {
			t.Fatalf("loadCache: %v", err)
		}
		if got.SessionID != hatch.SessionID {
			t.Fatalf("expected newer hatch cache %q, got %q", hatch.SessionID, got.SessionID)
		}
		if h, _ := (diskCacheStore{}).Load(slot); h == nil {
			t.Fatal("selected hatch must be preserved")
		}
	})

	t.Run("newer primary wins", func(t *testing.T) {
		primary := baseCache(slot, past.Add(time.Hour))
		hatch := baseCache(slot, past)
		plantBothStores(t, slot, primary, hatch)

		got, err := loadCache(slot)
		if err != nil {
			t.Fatalf("loadCache: %v", err)
		}
		if got.SessionID != primary.SessionID {
			t.Fatalf("expected newer primary cache %q, got %q", primary.SessionID, got.SessionID)
		}
		if h, _ := (diskCacheStore{}).Load(slot); h == nil {
			t.Fatal("ignored hatch must be preserved")
		}
	})

	t.Run("exact tie is actionable error", func(t *testing.T) {
		created := past.Truncate(time.Second)
		primary := baseCache(slot, created)
		primary.SessionID = "sess-a"
		hatch := baseCache(slot, created)
		hatch.SessionID = "sess-b"
		plantBothStores(t, slot, primary, hatch)

		if _, err := loadCache(slot); !errors.Is(err, errMultipleSessionCaches) {
			t.Fatalf("expected errMultipleSessionCaches on tie, got %v", err)
		}
		if h, _ := (diskCacheStore{}).Load(slot); h == nil {
			t.Fatal("tie must not delete either cache (hatch)")
		}
	})
}

// TestSelectedCacheStillValidated proves multi-cache selection does not relax
// the validation gate: a newer cache with a wrong key is selected, still fails
// salt/key verification, and never yields a usable key (ADR-0017 security).
func TestSelectedCacheStillValidated(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	configPath, dataPath := setupProject(t, "correct-secret")
	slot := vaultSlotFor(dataPath)
	sm := sessionManagerForTest(t, configPath, dataPath)
	defer sm.Close()

	timeout, _ := ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	good, err := sm.LoadCache()
	if err != nil || good == nil {
		t.Fatalf("load good cache: %v", err)
	}

	// A forged cache with a bogus key but a NEWER timestamp: selection prefers
	// it, yet verification must reject it instead of handing back a key.
	forged := baseCache(slot, good.CreatedAt.Add(time.Hour))
	forged.Salt = good.Salt
	forged.Key = base64.StdEncoding.EncodeToString(make([]byte, crypto.KeySize))
	if err := (diskCacheStore{}).Save(slot, forged); err != nil {
		t.Fatalf("save forged hatch: %v", err)
	}
	setActiveSessionStore(t, &fakeSessionStore{cache: good})

	selected, err := sm.LoadCache()
	if err != nil {
		t.Fatalf("loadCache: %v", err)
	}
	if selected.SessionID != forged.SessionID {
		t.Fatalf("expected forged newer cache to be selected, got %q", selected.SessionID)
	}

	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("forged newer cache must not yield a usable key")
	}
}

func resetHatchSelectionForTest(t *testing.T) {
	t.Helper()
	prev := HatchCacheSelected()
	hatchCacheSelectedFlag.Store(false)
	t.Cleanup(func() { hatchCacheSelectedFlag.Store(prev) })
}

func TestLoadCacheHatchSelectionFlagsWithoutWarning(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	slot := testSlot
	past := time.Now().Add(-2 * time.Hour)

	t.Run("hatch win sets flag without warning", func(t *testing.T) {
		resetHatchSelectionForTest(t)
		plantBothStores(t, slot, baseCache(slot, past), baseCache(slot, past.Add(time.Hour)))
		stderr := captureStderr(t)
		got, err := loadCache(slot)
		if err != nil {
			t.Fatalf("loadCache: %v", err)
		}
		if got.SessionID != "sess-"+past.Add(time.Hour).Format("150405.000") && got.CreatedAt.Before(past.Add(30*time.Minute)) {
			t.Fatalf("expected newer hatch cache, got created %v", got.CreatedAt)
		}
		if !HatchCacheSelected() {
			t.Fatal("hatch selection flag not set")
		}
		if strings.Contains(stderr(), "unencrypted on disk") {
			t.Fatal("hatch win must not warn on stderr; the warning belongs to session start")
		}
	})

	t.Run("secure store failure fallback flags without warning", func(t *testing.T) {
		resetHatchSelectionForTest(t)
		setActiveSessionStore(t, &fakeSessionStore{err: errors.New("keychain locked")})
		if err := (diskCacheStore{}).Save(slot, baseCache(slot, past)); err != nil {
			t.Fatalf("save hatch: %v", err)
		}
		stderr := captureStderr(t)
		got, err := loadCache(slot)
		if err != nil || got == nil {
			t.Fatalf("loadCache = (%v, %v), want hatch cache", got, err)
		}
		if !HatchCacheSelected() {
			t.Fatal("fallback flag not set")
		}
		if strings.Contains(stderr(), "unencrypted on disk") {
			t.Fatal("fallback to hatch must not warn on stderr; the warning belongs to session start")
		}
	})

	t.Run("primary win stays silent", func(t *testing.T) {
		resetHatchSelectionForTest(t)
		plantBothStores(t, slot, baseCache(slot, past.Add(time.Hour)), baseCache(slot, past))
		stderr := captureStderr(t)
		if _, err := loadCache(slot); err != nil {
			t.Fatalf("loadCache: %v", err)
		}
		if HatchCacheSelected() {
			t.Fatal("flag must stay unset when primary wins")
		}
		if strings.Contains(stderr(), "unencrypted on disk") {
			t.Fatal("primary win must not warn")
		}
	})
}
