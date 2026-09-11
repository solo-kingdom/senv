package session

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSessionStorePlatformSelection(t *testing.T) {
	store := defaultSessionStoreFor("0123456789abcdef")
	if _, ok := store.(tmpfsStore); !ok {
		t.Fatalf("default store = %T, want tmpfsStore", store)
	}
}

func forceDarwinDiskHatch(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	originalProbe := runtimeFilesystemProbe
	runtimeFilesystemProbe = func(string) (runtimeFilesystemKind, error) {
		return runtimeFilesystemUnknown, nil
	}
	t.Cleanup(func() { runtimeFilesystemProbe = originalProbe })
	originalOS := sessionHostOS
	sessionHostOS = "darwin"
	t.Cleanup(func() { sessionHostOS = originalOS })
	originalInsecure := insecureCacheEnabled
	insecureCacheEnabled = false
	t.Cleanup(func() { insecureCacheEnabled = originalInsecure })
}

func TestSaveCacheDarwinFallsBackToDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	originalProbe := runtimeFilesystemProbe
	runtimeFilesystemProbe = func(string) (runtimeFilesystemKind, error) {
		return runtimeFilesystemUnknown, nil
	}
	t.Cleanup(func() { runtimeFilesystemProbe = originalProbe })
	originalOS := sessionHostOS
	sessionHostOS = "darwin"
	t.Cleanup(func() { sessionHostOS = originalOS })
	originalInsecure := insecureCacheEnabled
	insecureCacheEnabled = false
	t.Cleanup(func() { insecureCacheEnabled = originalInsecure })

	stderr := captureStderr(t)
	if err := saveCache(testSlot, &SessionCache{SessionID: "darwin-hatch"}); err != nil {
		t.Fatalf("saveCache() darwin fallback: %v", err)
	}
	if !strings.Contains(stderr(), "unencrypted on disk") {
		t.Fatal("darwin fallback did not print the disk-hatch warning")
	}
	loaded, err := (diskCacheStore{}).Load(testSlot)
	if err != nil || loaded == nil || loaded.SessionID != "darwin-hatch" {
		t.Fatalf("disk hatch = (%v, %v), want darwin-hatch", loaded, err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".local", "share", "senv", "session")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy data-dir cache appeared: %v", err)
	}
}

func TestSaveCacheDarwinUsesTmpfsWhenProven(t *testing.T) {
	isolateSessionCache(t)
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	originalOS := sessionHostOS
	sessionHostOS = "darwin"
	t.Cleanup(func() { sessionHostOS = originalOS })
	originalInsecure := insecureCacheEnabled
	insecureCacheEnabled = false
	t.Cleanup(func() { insecureCacheEnabled = originalInsecure })

	if err := saveCache(testSlot, &SessionCache{SessionID: "darwin-tmpfs"}); err != nil {
		t.Fatalf("saveCache() proven tmpfs: %v", err)
	}
	if loaded, err := (diskCacheStore{}).Load(testSlot); err != nil || loaded != nil {
		t.Fatalf("proven tmpfs wrote disk hatch: loaded=%v err=%v", loaded, err)
	}
	loaded, err := (tmpfsStore{}).Load(testSlot)
	if err != nil || loaded == nil || loaded.SessionID != "darwin-tmpfs" {
		t.Fatalf("tmpfs = (%v, %v), want darwin-tmpfs", loaded, err)
	}
}

func TestSaveCacheLinuxUnprovenFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	originalProbe := runtimeFilesystemProbe
	runtimeFilesystemProbe = func(string) (runtimeFilesystemKind, error) {
		return runtimeFilesystemUnknown, nil
	}
	t.Cleanup(func() { runtimeFilesystemProbe = originalProbe })
	originalOS := sessionHostOS
	sessionHostOS = "linux"
	t.Cleanup(func() { sessionHostOS = originalOS })
	originalInsecure := insecureCacheEnabled
	insecureCacheEnabled = false
	t.Cleanup(func() { insecureCacheEnabled = originalInsecure })

	if err := saveCache(testSlot, &SessionCache{SessionID: "linux-closed"}); !errors.Is(err, ErrNoSecureSessionStore) {
		t.Fatalf("saveCache() linux unproven = %v, want ErrNoSecureSessionStore", err)
	}
	if loaded, err := (diskCacheStore{}).Load(testSlot); err != nil || loaded != nil {
		t.Fatalf("linux fail-closed wrote disk: loaded=%v err=%v", loaded, err)
	}
}

func captureStderr(t *testing.T) func() string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = original
		_ = writer.Close()
	})
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, reader)
		close(done)
	}()
	return func() string {
		_ = writer.Close()
		<-done
		os.Stderr = original
		return buf.String()
	}
}
