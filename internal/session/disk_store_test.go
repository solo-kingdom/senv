package session

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSlot = "0123456789abcdef"

type fakeSessionStore struct {
	cache         *SessionCache
	err           error
	cleared       bool
	clearAll      bool
	legacy        *SessionCache
	legacyCleared bool
	slotSeen      string
}

func (f *fakeSessionStore) Save(slot string, cache *SessionCache) error {
	f.slotSeen = slot
	f.cache = cache
	return nil
}
func (f *fakeSessionStore) Load(slot string) (*SessionCache, error) {
	f.slotSeen = slot
	return f.cache, f.err
}
func (f *fakeSessionStore) Clear(slot string) error { f.cleared = true; return nil }
func (f *fakeSessionStore) ClearAll() error         { f.clearAll = true; return nil }
func (f *fakeSessionStore) LoadLegacy() (*SessionCache, error) {
	return f.legacy, nil
}
func (f *fakeSessionStore) ClearLegacy() error { f.legacyCleared = true; return nil }

func setActiveSessionStore(t *testing.T, store SessionStore) {
	t.Helper()
	original := activeSessionStoreFor
	activeSessionStoreFor = func(string) SessionStore { return store }
	t.Cleanup(func() { activeSessionStoreFor = original })
}

func TestDiskCacheStoreSavePermissionsAndReplace(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	store := diskCacheStore{}

	first := &SessionCache{SessionID: "sess-first"}
	if err := store.Save(testSlot, first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	second := &SessionCache{SessionID: "sess-second"}
	if err := store.Save(testSlot, second); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	path := filepath.Join(os.Getenv("XDG_CACHE_HOME"), diskCacheDirName, diskCacheFileName(testSlot))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat cache dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 700", dirInfo.Mode().Perm())
	}
	loaded, err := store.Load(testSlot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded == nil || loaded.SessionID != "sess-second" {
		t.Fatalf("loaded = %+v, want second session", loaded)
	}
}

func TestDiskCacheSlotsAreIsolated(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	store := diskCacheStore{}
	other := "fedcba9876543210"

	if err := store.Save(testSlot, &SessionCache{SessionID: "first"}); err != nil {
		t.Fatalf("save first slot: %v", err)
	}
	if err := store.Save(other, &SessionCache{SessionID: "second"}); err != nil {
		t.Fatalf("save second slot: %v", err)
	}
	first, err := store.Load(testSlot)
	if err != nil || first == nil || first.SessionID != "first" {
		t.Fatalf("first slot = (%v, %v), want first session", first, err)
	}
	second, err := store.Load(other)
	if err != nil || second == nil || second.SessionID != "second" {
		t.Fatalf("second slot = (%v, %v), want second session", second, err)
	}
	if err := store.Clear(testSlot); err != nil {
		t.Fatalf("clear first slot: %v", err)
	}
	if left, err := store.Load(other); err != nil || left == nil {
		t.Fatalf("clearing one slot removed another: (%v, %v)", left, err)
	}
}

func TestDiskCacheStoreLoadMissingAndCorrupt(t *testing.T) {
	isolateSessionCache(t)
	cacheBase := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheBase)
	store := diskCacheStore{}

	loaded, err := store.Load(testSlot)
	if err != nil || loaded != nil {
		t.Fatalf("Load() missing = (%v, %v), want (nil, nil)", loaded, err)
	}

	dir := filepath.Join(cacheBase, diskCacheDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(dir, diskCacheFileName(testSlot))
	if err := os.WriteFile(corrupt, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(testSlot)
	if !errors.Is(err, ErrSessionUnverifiable) || loaded != nil {
		t.Fatalf("Load() corrupt = (%v, %v), want unverifiable", loaded, err)
	}
	if _, err := os.Stat(corrupt); err != nil {
		t.Fatalf("corrupt cache was removed: %v", err)
	}
}

func TestLoadCacheRejectsMultipleStores(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setActiveSessionStore(t, &fakeSessionStore{cache: &SessionCache{SessionID: "primary"}})
	if err := (diskCacheStore{}).Save(testSlot, &SessionCache{SessionID: "hatch"}); err != nil {
		t.Fatalf("save hatch: %v", err)
	}

	if _, err := loadCache(testSlot); !errors.Is(err, errMultipleSessionCaches) {
		t.Fatalf("loadCache() error = %v, want errMultipleSessionCaches", err)
	}
}

func TestLoadCacheFallsBackToHatchWhenPlatformStoreUnavailable(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setActiveSessionStore(t, &fakeSessionStore{err: ErrNoSecureSessionStore})

	if loaded, err := loadCache(testSlot); !errors.Is(err, ErrNoSecureSessionStore) || loaded != nil {
		t.Fatalf("loadCache() without hatch = (%v, %v), want platform error", loaded, err)
	}

	if err := (diskCacheStore{}).Save(testSlot, &SessionCache{SessionID: "hatch-only"}); err != nil {
		t.Fatalf("save hatch: %v", err)
	}
	loaded, err := loadCache(testSlot)
	if err != nil || loaded == nil || loaded.SessionID != "hatch-only" {
		t.Fatalf("loadCache() with hatch = (%v, %v), want hatch session", loaded, err)
	}
}

// TestLegacyNoticeIsShownOnce asserts the unmatched-legacy hint is written to
// stderr exactly once and leaves a runtime marker so later reads stay silent.
func TestLegacyNoticeIsShownOnce(t *testing.T) {
	isolateSessionCache(t)

	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = writer
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()

	notifyLegacyCacheOnce()
	notifyLegacyCacheOnce()

	_ = writer.Close()
	os.Stderr = originalStderr
	out := <-done

	if got := strings.Count(out, legacyCacheNoticeMessage); got != 1 {
		t.Fatalf("legacy notice shown %d times, want 1: %q", got, out)
	}
	marker := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "senv", legacyNoticeFileName)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("notice marker missing: %v", err)
	}
}

func TestLoadCacheAdoptsMatchingLegacySlot(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	store := &fakeSessionStore{legacy: &SessionCache{SessionID: "legacy", DataPathHash: testSlot}}
	setActiveSessionStore(t, store)

	adopted, err := loadCache(testSlot)
	if err != nil || adopted == nil || adopted.SessionID != "legacy" {
		t.Fatalf("loadCache() = (%v, %v), want adopted legacy session", adopted, err)
	}
	if !store.legacyCleared {
		t.Fatal("adopted legacy entry was not cleared from the source store")
	}
}

func TestLoadCacheKeepsUnmatchedLegacySlot(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	store := &fakeSessionStore{legacy: &SessionCache{SessionID: "other", DataPathHash: "ffffffffffffffff"}}
	setActiveSessionStore(t, store)

	loaded, err := loadCache(testSlot)
	if err != nil || loaded != nil {
		t.Fatalf("loadCache() = (%v, %v), want no session", loaded, err)
	}
	if store.legacyCleared {
		t.Fatal("unmatched legacy cache must be preserved")
	}
}

func TestClearCacheClearsAllStores(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fake := &fakeSessionStore{}
	setActiveSessionStore(t, fake)
	if err := (diskCacheStore{}).Save(testSlot, &SessionCache{SessionID: "hatch"}); err != nil {
		t.Fatalf("save hatch: %v", err)
	}

	if err := clearCache(testSlot); err != nil {
		t.Fatalf("clearCache() error = %v", err)
	}
	if !fake.cleared {
		t.Fatal("platform store was not cleared")
	}
	loaded, err := (diskCacheStore{}).Load(testSlot)
	if err != nil || loaded != nil {
		t.Fatalf("disk cache survived clear: loaded = %v, err = %v", loaded, err)
	}
}

func TestClearAllCachesClearsEverySlot(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fake := &fakeSessionStore{}
	setActiveSessionStore(t, fake)
	if err := (diskCacheStore{}).Save(testSlot, &SessionCache{SessionID: "one"}); err != nil {
		t.Fatalf("save first slot: %v", err)
	}
	if err := (diskCacheStore{}).Save("fedcba9876543210", &SessionCache{SessionID: "two"}); err != nil {
		t.Fatalf("save second slot: %v", err)
	}

	if err := clearAllCaches(); err != nil {
		t.Fatalf("clearAllCaches() error = %v", err)
	}
	if !fake.clearAll {
		t.Fatal("platform store ClearAll was not called")
	}
	for _, slot := range []string{testSlot, "fedcba9876543210"} {
		if loaded, err := (diskCacheStore{}).Load(slot); err != nil || loaded != nil {
			t.Fatalf("slot %s survived clear --all: %v (%v)", slot, loaded, err)
		}
	}
}

func TestDiskCacheBootIDInvalidation(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dataPath := t.TempDir()
	slot := vaultSlotFor(dataPath)
	manager := NewManager("", dataPath)
	cache := &SessionCache{
		DataPathHash: slot,
		BootID:       "stale-boot",
		TimeoutType:  string(TimeoutRestart),
	}
	if err := (diskCacheStore{}).Save(slot, cache); err != nil {
		t.Fatalf("save hatch: %v", err)
	}

	loaded, err := loadCache(slot)
	if err != nil || loaded == nil {
		t.Fatalf("loadCache() = (%v, %v), want saved cache", loaded, err)
	}
	valid, err := manager.IsCacheValid(loaded)
	if err != nil {
		t.Fatalf("IsCacheValid() error = %v", err)
	}
	if valid {
		t.Fatal("stale boot ID was accepted for disk cache")
	}
}

func TestInsecureCacheOptInRedirectsWrites(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setActiveSessionStore(t, &fakeSessionStore{})
	original := insecureCacheEnabled
	insecureCacheEnabled = false
	t.Cleanup(func() { insecureCacheEnabled = original })

	cache := &SessionCache{SessionID: "redirected"}
	if err := saveCache(testSlot, cache); err != nil {
		t.Fatalf("default save: %v", err)
	}
	if loaded, err := (diskCacheStore{}).Load(testSlot); err != nil || loaded != nil {
		t.Fatalf("default write touched disk: loaded = %v, err = %v", loaded, err)
	}

	EnableInsecureCache()
	if err := saveCache(testSlot, cache); err != nil {
		t.Fatalf("opt-in save: %v", err)
	}
	loaded, err := (diskCacheStore{}).Load(testSlot)
	if err != nil || loaded == nil || loaded.SessionID != "redirected" {
		t.Fatalf("opt-in write missed disk: loaded = %v, err = %v", loaded, err)
	}
}
