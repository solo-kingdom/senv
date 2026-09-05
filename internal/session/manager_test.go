package session

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/storage"
)

func isolateSessionCache(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	originalProbe := runtimeFilesystemProbe
	runtimeFilesystemProbe = func(string) (runtimeFilesystemKind, error) {
		return runtimeFilesystemMemory, nil
	}
	t.Cleanup(func() { runtimeFilesystemProbe = originalProbe })
}

func setupProject(t *testing.T, password string) (configPath, dataPath string) {
	t.Helper()
	dir := t.TempDir()
	configPath = filepath.Join(dir, "cfg")
	dataPath = filepath.Join(dir, "data")
	if err := os.MkdirAll(configPath, 0o700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	mgr := storage.NewManager(configPath, dataPath)
	if err := mgr.Initialize(password); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return configPath, dataPath
}

func TestGetCachedKeyRejectsStaleKey(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")

	timeout, err := ParseTimeout("restart")
	if err != nil || timeout == nil {
		t.Fatalf("parse timeout: %v", err)
	}

	sm := sessionManagerForTest(t, configPath, dataPath)
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	storageMgr := storage.NewManager(configPath, dataPath)
	metadata, err := storageMgr.LoadMetadata()
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}

	staleCache := &SessionCache{
		Key:          base64.StdEncoding.EncodeToString(make([]byte, crypto.KeySize)),
		Salt:         metadata.Salt,
		CreatedAt:    time.Now(),
		TimeoutType:  string(TimeoutRestart),
		DataPathHash: hashDataPath(dataPath),
		SessionID:    "sess-stale-key",
	}
	if err := saveCache(staleCache); err != nil {
		t.Fatalf("save stale cache: %v", err)
	}

	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("expected stale key to be rejected")
	}

	valid, err := storageMgr.VerifyPassword("correct-secret")
	if err != nil {
		t.Fatalf("verify password: %v", err)
	}
	if !valid {
		t.Fatal("password should still verify after stale cache rejection")
	}
}

func TestGetCachedKeyRejectsStaleSalt(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")

	timeout, err := ParseTimeout("restart")
	if err != nil || timeout == nil {
		t.Fatalf("parse timeout: %v", err)
	}

	sm := sessionManagerForTest(t, configPath, dataPath)
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	storageMgr := storage.NewManager(configPath, dataPath)
	metadata, err := storageMgr.LoadMetadata()
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}

	newSalt, err := crypto.GenerateSalt()
	if err != nil {
		t.Fatalf("generate salt: %v", err)
	}
	metadata.Salt = base64.StdEncoding.EncodeToString(newSalt)
	if err := storageMgr.SaveMetadata(metadata); err != nil {
		t.Fatalf("save metadata: %v", err)
	}

	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("expected stale session to be rejected")
	}
}

func TestGetCachedKeyRejectsExpiredSession(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")

	sm := sessionManagerForTest(t, configPath, dataPath)
	cache := &SessionCache{
		Key:          base64.StdEncoding.EncodeToString(make([]byte, crypto.KeySize)),
		Salt:         "stale",
		CreatedAt:    time.Now().Add(-2 * time.Hour),
		ExpiresAt:    time.Now().Add(-time.Hour),
		TimeoutType:  string(TimeoutDuration),
		DataPathHash: hashDataPath(dataPath),
		SessionID:    "sess-test-expired",
	}
	if err := saveCache(cache); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestLoadCacheForDataPathIgnoresOtherProject(t *testing.T) {
	isolateSessionCache(t)
	configA, dataA := setupProject(t, "secret-a")
	_, dataB := setupProject(t, "secret-b")

	timeout, err := ParseTimeout("restart")
	if err != nil || timeout == nil {
		t.Fatalf("parse timeout: %v", err)
	}

	smA := sessionManagerForTest(t, configA, dataA)
	if err := smA.StartSession("secret-a", timeout); err != nil {
		t.Fatalf("start session A: %v", err)
	}

	smB := sessionManagerForTest(t, configA, dataB)
	if _, err := smB.GetCachedKey(); err == nil {
		t.Fatal("project B must not reuse project A session")
	}
}

func sessionManagerForTest(t *testing.T, configPath, dataPath string) *Manager {
	t.Helper()
	return NewManager(configPath, dataPath)
}

// --- Error classification (task 1.3) ---
// Each state must map to a distinct sentinel so the cmd layer can tell
// "just re-authenticate" apart from "your data may be desynced".

func TestErrorClass_NoCache(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, cfg, data)

	_, err := sm.GetCachedKey()
	if !errors.Is(err, ErrNoSession) {
		t.Fatalf("expected ErrNoSession, got %v", err)
	}
}

func TestErrorClass_Expired(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, cfg, data)

	// Plant a duration cache that already expired.
	cache := &SessionCache{
		Key:          base64.StdEncoding.EncodeToString(make([]byte, crypto.KeySize)),
		Salt:         "stale",
		CreatedAt:    time.Now().Add(-2 * time.Hour),
		ExpiresAt:    time.Now().Add(-time.Hour),
		TimeoutType:  string(TimeoutDuration),
		DataPathHash: hashDataPath(data),
		SessionID:    "sess-expired",
	}
	if err := saveCache(cache); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	_, err := sm.GetCachedKey()
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

func TestErrorClass_StaleMetadata(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	to, _ := ParseTimeout("restart")
	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("start session: %v", err)
	}

	// Rotate metadata salt -> cache salt no longer matches.
	storeMgr := storage.NewManager(cfg, data)
	md, _ := storeMgr.LoadMetadata()
	newSalt, _ := crypto.GenerateSalt()
	md.Salt = base64.StdEncoding.EncodeToString(newSalt)
	if err := storeMgr.SaveMetadata(md); err != nil {
		t.Fatalf("save metadata: %v", err)
	}

	_, err := sm.GetCachedKey()
	if !errors.Is(err, ErrSessionStaleMetadata) {
		t.Fatalf("expected ErrSessionStaleMetadata, got %v", err)
	}
}

func TestErrorClass_StaleKey(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	to, _ := ParseTimeout("restart")
	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("start session: %v", err)
	}

	// Keep salt identical but replace password_key with one for a different key,
	// so VerifyKey fails while salt still matches.
	storeMgr := storage.NewManager(cfg, data)
	md, _ := storeMgr.LoadMetadata()
	otherSalt, _ := crypto.GenerateSalt()
	otherKey := crypto.DeriveKey("other-password", otherSalt)
	pk, err := crypto.Encrypt(otherKey, []byte(crypto.HashPassword("other-password")))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	md.PasswordKey = pk
	if err := storeMgr.SaveMetadata(md); err != nil {
		t.Fatalf("save metadata: %v", err)
	}

	_, err = sm.GetCachedKey()
	if !errors.Is(err, ErrSessionStaleKey) {
		t.Fatalf("expected ErrSessionStaleKey, got %v", err)
	}
}

// --- Non-destructive stale handling (task 2.3) ---
// A stale cache must survive a failed GetCachedKey so it can still serve as a
// recovery key. Only the expired branch is allowed to clear.

func TestStaleNoClear_MetadataMismatchKeepsCache(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	to, _ := ParseTimeout("restart")
	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("start session: %v", err)
	}

	storeMgr := storage.NewManager(cfg, data)
	md, _ := storeMgr.LoadMetadata()
	newSalt, _ := crypto.GenerateSalt()
	md.Salt = base64.StdEncoding.EncodeToString(newSalt)
	_ = storeMgr.SaveMetadata(md)

	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("expected stale error")
	}

	// Cache must still be on disk and peekable.
	key, cache, err := sm.PeekCachedKey()
	if err != nil {
		t.Fatalf("PeekCachedKey after stale failure: %v", err)
	}
	if cache == nil || len(key) == 0 {
		t.Fatal("stale cache must remain readable as recovery key")
	}
}

func TestStaleNoClear_KeyInvalidKeepsCache(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	to, _ := ParseTimeout("restart")
	sm := sessionManagerForTest(t, cfg, data)
	if err := sm.StartSession("correct-secret", to); err != nil {
		t.Fatalf("start session: %v", err)
	}

	storeMgr := storage.NewManager(cfg, data)
	md, _ := storeMgr.LoadMetadata()
	otherKey := crypto.DeriveKey("other", make([]byte, crypto.SaltSize))
	pk, _ := crypto.Encrypt(otherKey, []byte(crypto.HashPassword("other")))
	md.PasswordKey = pk
	_ = storeMgr.SaveMetadata(md)

	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("expected stale error")
	}
	if _, cache, err := sm.PeekCachedKey(); err != nil || cache == nil {
		t.Fatalf("stale cache must survive key-invalid failure: err=%v cache=%v", err, cache)
	}
}

func TestStartSessionRekeyConcurrentLease(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	timeout, err := ParseTimeout("restart")
	if err != nil {
		t.Fatal(err)
	}
	store := storage.NewManager(configPath, dataPath)
	entered, release := make(chan struct{}), make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- store.WithVaultMutation(func(*storage.Manager) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	startDone := make(chan error, 1)
	manager := sessionManagerForTest(t, configPath, dataPath)
	go func() { startDone <- manager.StartSession("correct-secret", timeout) }()
	select {
	case err := <-startDone:
		t.Fatalf("StartSession crossed vault mutation lease: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("StartSession after lease release: %v", err)
	}
	if _, err := manager.GetCachedKey(); err != nil {
		t.Fatalf("successful session cache is not immediately valid: %v", err)
	}
}

// TestLegacyNeverCacheIsExpired ensures caches written by older versions with
// timeout_type "never" are classified as expired (unknown type): no crash, no
// password/data-desync misreport, and the caller is sent back to session start.
func TestLegacyNeverCacheIsExpired(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")

	bootID, err := systemBootID()
	if err != nil {
		t.Fatalf("boot id: %v", err)
	}
	legacy := &SessionCache{
		Key:          base64.StdEncoding.EncodeToString(make([]byte, crypto.KeySize)),
		Salt:         "stale",
		CreatedAt:    time.Now(),
		TimeoutType:  "never", // written by an older senv version
		BootID:       bootID,
		DataPathHash: hashDataPath(dataPath),
		SessionID:    "sess-legacy-never",
	}
	if err := saveCache(legacy); err != nil {
		t.Fatalf("save legacy cache: %v", err)
	}

	sm := sessionManagerForTest(t, configPath, dataPath)
	valid, err := sm.IsCacheValid(legacy)
	if valid {
		t.Fatal("legacy never cache must not validate")
	}
	if err == nil || !strings.Contains(err.Error(), "unknown timeout type") {
		t.Fatalf("expected unknown timeout type error, got valid=%v err=%v", valid, err)
	}

	if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired for legacy never cache, got %v", err)
	}
}
