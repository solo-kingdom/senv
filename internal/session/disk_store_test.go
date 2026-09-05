package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeSessionStore struct {
	cache   *SessionCache
	cleared bool
	err     error
}

func (f *fakeSessionStore) Save(*SessionCache) error     { return nil }
func (f *fakeSessionStore) Load() (*SessionCache, error) { return f.cache, f.err }
func (f *fakeSessionStore) Clear() error                 { f.cleared = true; return nil }

func setActiveSessionStore(t *testing.T, store SessionStore) {
	t.Helper()
	original := activeSessionStore
	activeSessionStore = store
	t.Cleanup(func() { activeSessionStore = original })
}

func TestDiskCacheStoreSavePermissionsAndReplace(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	store := diskCacheStore{}

	first := &SessionCache{SessionID: "sess-first"}
	if err := store.Save(first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	second := &SessionCache{SessionID: "sess-second"}
	if err := store.Save(second); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	path := filepath.Join(os.Getenv("XDG_CACHE_HOME"), diskCacheDirName, "session.json")
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
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded == nil || loaded.SessionID != "sess-second" {
		t.Fatalf("loaded = %+v, want second session", loaded)
	}
}

func TestDiskCacheStoreLoadMissingAndCorrupt(t *testing.T) {
	isolateSessionCache(t)
	cacheBase := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheBase)
	store := diskCacheStore{}

	loaded, err := store.Load()
	if err != nil || loaded != nil {
		t.Fatalf("Load() missing = (%v, %v), want (nil, nil)", loaded, err)
	}

	dir := filepath.Join(cacheBase, diskCacheDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load()
	if err != nil || loaded != nil {
		t.Fatalf("Load() corrupt = (%v, %v), want (nil, nil)", loaded, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); err != nil {
		t.Fatalf("corrupt cache was removed: %v", err)
	}
}

func TestLoadCacheRejectsMultipleStores(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setActiveSessionStore(t, &fakeSessionStore{cache: &SessionCache{SessionID: "primary"}})
	if err := (diskCacheStore{}).Save(&SessionCache{SessionID: "hatch"}); err != nil {
		t.Fatalf("save hatch: %v", err)
	}

	if _, err := loadCache(); !errors.Is(err, errMultipleSessionCaches) {
		t.Fatalf("loadCache() error = %v, want errMultipleSessionCaches", err)
	}
}

func TestLoadCacheFallsBackToHatchWhenPlatformStoreUnavailable(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setActiveSessionStore(t, &fakeSessionStore{err: ErrNoSecureSessionStore})

	if loaded, err := loadCache(); !errors.Is(err, ErrNoSecureSessionStore) || loaded != nil {
		t.Fatalf("loadCache() without hatch = (%v, %v), want platform error", loaded, err)
	}

	if err := (diskCacheStore{}).Save(&SessionCache{SessionID: "hatch-only"}); err != nil {
		t.Fatalf("save hatch: %v", err)
	}
	loaded, err := loadCache()
	if err != nil || loaded == nil || loaded.SessionID != "hatch-only" {
		t.Fatalf("loadCache() with hatch = (%v, %v), want hatch session", loaded, err)
	}
}

func TestClearCacheClearsAllStores(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fake := &fakeSessionStore{}
	setActiveSessionStore(t, fake)
	if err := (diskCacheStore{}).Save(&SessionCache{SessionID: "hatch"}); err != nil {
		t.Fatalf("save hatch: %v", err)
	}

	if err := clearCache(); err != nil {
		t.Fatalf("clearCache() error = %v", err)
	}
	if !fake.cleared {
		t.Fatal("platform store was not cleared")
	}
	loaded, err := (diskCacheStore{}).Load()
	if err != nil || loaded != nil {
		t.Fatalf("disk cache survived clear: loaded = %v, err = %v", loaded, err)
	}
}

func TestDiskCacheBootIDInvalidation(t *testing.T) {
	isolateSessionCache(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dataPath := t.TempDir()
	manager := NewManager("", dataPath)
	cache := &SessionCache{
		DataPathHash: hashDataPath(dataPath),
		BootID:       "stale-boot",
		TimeoutType:  string(TimeoutRestart),
	}
	if err := (diskCacheStore{}).Save(cache); err != nil {
		t.Fatalf("save hatch: %v", err)
	}

	loaded, err := loadCache()
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
	if err := saveCache(cache); err != nil {
		t.Fatalf("default save: %v", err)
	}
	if loaded, err := (diskCacheStore{}).Load(); err != nil || loaded != nil {
		t.Fatalf("default write touched disk: loaded = %v, err = %v", loaded, err)
	}

	EnableInsecureCache()
	if err := saveCache(cache); err != nil {
		t.Fatalf("opt-in save: %v", err)
	}
	loaded, err := (diskCacheStore{}).Load()
	if err != nil || loaded == nil || loaded.SessionID != "redirected" {
		t.Fatalf("opt-in write missed disk: loaded = %v, err = %v", loaded, err)
	}
}
