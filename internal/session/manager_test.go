package session

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		DataPathHash: vaultSlotFor(dataPath),
		SessionID:    "sess-stale-key",
	}
	if err := saveCache(vaultSlotFor(dataPath), staleCache); err != nil {
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
		DataPathHash: vaultSlotFor(dataPath),
		SessionID:    "sess-test-expired",
	}
	if err := saveCache(vaultSlotFor(dataPath), cache); err != nil {
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
		DataPathHash: vaultSlotFor(data),
		SessionID:    "sess-expired",
	}
	if err := saveCache(vaultSlotFor(data), cache); err != nil {
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

// TestLegacyNeverCacheIsAdoptedAsRestart ensures caches written by older
// versions with timeout_type "never" are adopted (equivalent to restart)
// instead of forcing a re-authentication.
func TestLegacyNeverCacheIsAdoptedAsRestart(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")

	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("restart")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	cache, err := sm.LoadCache()
	if err != nil || cache == nil {
		t.Fatalf("LoadCache: cache=%v err=%v", cache, err)
	}
	legacy := *cache
	legacy.TimeoutType = "never" // written by an older senv version
	if err := saveCache(vaultSlotFor(dataPath), &legacy); err != nil {
		t.Fatalf("save legacy cache: %v", err)
	}

	valid, err := sm.IsCacheValid(&legacy)
	if err != nil || !valid {
		t.Fatalf("legacy never cache must validate as restart: valid=%v err=%v", valid, err)
	}
	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("legacy never cache must be reusable: %v", err)
	}

	// A changed boot ID invalidates restart/never sessions.
	original := systemBootID
	systemBootID = func() (string, error) { return "boot-changed", nil }
	t.Cleanup(func() { systemBootID = original })
	if err := saveCache(vaultSlotFor(dataPath), &legacy); err != nil {
		t.Fatalf("re-save legacy cache: %v", err)
	}
	if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionInvalidated) {
		t.Fatalf("expected ErrSessionInvalidated after reboot, got %v", err)
	}
}

func TestUnverifiableBootIDKeepsCache(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("restart")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	original := systemBootID
	systemBootID = func() (string, error) { return "", errors.New("boot id unavailable") }
	t.Cleanup(func() { systemBootID = original })

	if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionUnverifiable) {
		t.Fatalf("expected ErrSessionUnverifiable, got %v", err)
	}
	if _, cache, err := sm.PeekCachedKey(); err != nil || cache == nil {
		t.Fatalf("unverifiable failure must keep the cache: cache=%v err=%v", cache, err)
	}

	systemBootID = original
	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("session must be reusable once the environment recovers: %v", err)
	}
}

func TestDurationSessionSurvivesReboot(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	original := systemBootID
	systemBootID = func() (string, error) { return "boot-changed", nil }
	t.Cleanup(func() { systemBootID = original })

	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("duration session must survive a reboot: %v", err)
	}
}

func TestDurationSessionOnDiskHatchSurvivesReboot(t *testing.T) {
	forceDarwinDiskHatch(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if loaded, err := (diskCacheStore{}).Load(vaultSlotFor(dataPath)); err != nil || loaded == nil {
		t.Fatalf("expected disk hatch session, got (%v, %v)", loaded, err)
	}

	original := systemBootID
	systemBootID = func() (string, error) { return "boot-changed", nil }
	t.Cleanup(func() { systemBootID = original })

	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("duration disk-hatch session must survive a reboot: %v", err)
	}
}

func TestRestartSessionOnDiskHatchInvalidatesAfterReboot(t *testing.T) {
	forceDarwinDiskHatch(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("restart")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	original := systemBootID
	systemBootID = func() (string, error) { return "boot-changed", nil }
	t.Cleanup(func() { systemBootID = original })

	if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionInvalidated) {
		t.Fatalf("restart disk-hatch session must invalidate after reboot, got %v", err)
	}
}

func TestSessionsArePerVault(t *testing.T) {
	isolateSessionCache(t)
	cfgA, dataA := setupProject(t, "secret-a")
	cfgB, dataB := setupProject(t, "secret-b")
	timeout, _ := ParseTimeout("restart")

	smA := sessionManagerForTest(t, cfgA, dataA)
	smB := sessionManagerForTest(t, cfgB, dataB)
	if err := smA.StartSession("secret-a", timeout); err != nil {
		t.Fatalf("start A: %v", err)
	}
	if err := smB.StartSession("secret-b", timeout); err != nil {
		t.Fatalf("start B: %v", err)
	}
	if _, err := smA.GetCachedKey(); err != nil {
		t.Fatalf("vault A session was overwritten: %v", err)
	}
	if _, err := smB.GetCachedKey(); err != nil {
		t.Fatalf("vault B session is not reusable: %v", err)
	}
	cacheA, _ := smA.LoadCache()
	cacheB, _ := smB.LoadCache()
	if cacheA == nil || cacheB == nil || cacheA.DataPathHash == cacheB.DataPathHash {
		t.Fatal("vault sessions must occupy distinct slots")
	}
}

func TestRenewalSlidesExpiryWithinCap(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	cache, _ := sm.LoadCache()
	if cache == nil || cache.TimeoutSeconds != int64((8*time.Hour).Seconds()) {
		t.Fatalf("cache missing timeout_seconds: %+v", cache)
	}
	created := cache.CreatedAt

	original := timeNow
	t.Cleanup(func() { timeNow = original })
	timeNow = func() time.Time { return created.Add(7 * time.Hour) }
	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("GetCachedKey: %v", err)
	}
	renewed, _ := sm.LoadCache()
	want := created.Add(15 * time.Hour)
	if !renewed.ExpiresAt.Equal(want) {
		t.Fatalf("expiry = %s, want %s", renewed.ExpiresAt, want)
	}

	timeNow = func() time.Time { return created.Add(14 * time.Hour) }
	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("GetCachedKey: %v", err)
	}
	slid, _ := sm.LoadCache()
	if !slid.ExpiresAt.Equal(created.Add(22 * time.Hour)) {
		t.Fatalf("expiry = %s, want %s", slid.ExpiresAt, created.Add(22*time.Hour))
	}

	// Past the ceiling, renewal must not cross created_at + 24h.
	timeNow = func() time.Time { return created.Add(16*time.Hour + 30*time.Minute) }
	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("GetCachedKey: %v", err)
	}
	capped, _ := sm.LoadCache()
	if !capped.ExpiresAt.Equal(created.Add(24 * time.Hour)) {
		t.Fatalf("expiry = %s, want ceiling %s", capped.ExpiresAt, created.Add(24*time.Hour))
	}
}

func TestReadOnlyStatusDoesNotRenew(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	before, _ := sm.LoadCache()

	original := timeNow
	t.Cleanup(func() { timeNow = original })
	timeNow = func() time.Time { return before.CreatedAt.Add(7 * time.Hour) }
	for i := 0; i < 3; i++ {
		if status := sm.DescribeCache(); status.State != StateActive {
			t.Fatalf("DescribeCache state = %s, want active", status.State)
		}
	}
	after, _ := sm.LoadCache()
	if !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatalf("read-only commands renewed expiry: %s -> %s", before.ExpiresAt, after.ExpiresAt)
	}
}

func TestLegacyCacheWithoutTimeoutIsNotRenewed(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	timeout, _ := ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	cache, _ := sm.LoadCache()
	legacy := *cache
	legacy.TimeoutSeconds = 0
	if err := saveCache(vaultSlotFor(dataPath), &legacy); err != nil {
		t.Fatalf("save legacy cache: %v", err)
	}

	original := timeNow
	t.Cleanup(func() { timeNow = original })
	timeNow = func() time.Time { return legacy.CreatedAt.Add(7 * time.Hour) }
	if _, err := sm.GetCachedKey(); err != nil {
		t.Fatalf("legacy cache must still be reusable: %v", err)
	}
	after, _ := sm.LoadCache()
	if !after.ExpiresAt.Equal(legacy.ExpiresAt) {
		t.Fatalf("legacy cache expiry changed: %s -> %s", legacy.ExpiresAt, after.ExpiresAt)
	}
}

// TestGetCachedKeyPreservesOnInvalidated covers the tightened clearing boundary
// (ADR-0017): a "restart" session that meets a changed boot ID, and a cache
// bound to a different vault, must report ErrSessionInvalidated while leaving
// the cache on disk. Only ReasonExpired may auto-clear.
func TestGetCachedKeyPreservesOnInvalidated(t *testing.T) {
	t.Run("reboot keeps restart cache", func(t *testing.T) {
		isolateSessionCache(t)
		configPath, dataPath := setupProject(t, "correct-secret")
		sm := sessionManagerForTest(t, configPath, dataPath)
		defer sm.Close()

		timeout, _ := ParseTimeout("restart")
		if err := sm.StartSession("correct-secret", timeout); err != nil {
			t.Fatalf("start session: %v", err)
		}

		original := systemBootID
		systemBootID = func() (string, error) { return "boot-changed", nil }
		t.Cleanup(func() { systemBootID = original })

		if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionInvalidated) {
			t.Fatalf("expected ErrSessionInvalidated, got %v", err)
		}
		if cache, err := sm.LoadCache(); err != nil || cache == nil {
			t.Fatalf("invalidated cache must be preserved: cache=%v err=%v", cache, err)
		}
	})

	t.Run("vault mismatch keeps cache", func(t *testing.T) {
		isolateSessionCache(t)
		configPath, dataPath := setupProject(t, "correct-secret")
		otherData := t.TempDir()
		sm := sessionManagerForTest(t, configPath, dataPath)
		defer sm.Close()

		timeout, _ := ParseTimeout("restart")
		if err := sm.StartSession("correct-secret", timeout); err != nil {
			t.Fatalf("start session: %v", err)
		}

		// Rebind the same cache to a different vault slot.
		cache, err := sm.LoadCache()
		if err != nil || cache == nil {
			t.Fatalf("load cache: cache=%v err=%v", cache, err)
		}
		moved := *cache
		moved.DataPathHash = vaultSlotFor(otherData)
		if err := saveCache(vaultSlotFor(dataPath), &moved); err != nil {
			t.Fatalf("save moved cache: %v", err)
		}

		if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionInvalidated) {
			t.Fatalf("expected ErrSessionInvalidated, got %v", err)
		}
		if cache, err := sm.LoadCache(); err != nil || cache == nil {
			t.Fatalf("vault-mismatched cache must be preserved: cache=%v err=%v", cache, err)
		}
	})

	t.Run("expired still clears", func(t *testing.T) {
		isolateSessionCache(t)
		configPath, dataPath := setupProject(t, "correct-secret")
		sm := sessionManagerForTest(t, configPath, dataPath)
		defer sm.Close()

		timeout, _ := ParseTimeout("8h")
		if err := sm.StartSession("correct-secret", timeout); err != nil {
			t.Fatalf("start session: %v", err)
		}
		cache, err := sm.LoadCache()
		if err != nil || cache == nil {
			t.Fatalf("load cache: cache=%v err=%v", cache, err)
		}
		expired := *cache
		expired.ExpiresAt = time.Now().Add(-time.Minute)
		if err := saveCache(vaultSlotFor(dataPath), &expired); err != nil {
			t.Fatalf("save expired cache: %v", err)
		}

		if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionExpired) {
			t.Fatalf("expected ErrSessionExpired, got %v", err)
		}
		if _, err := sm.LoadCache(); err != nil {
			t.Fatalf("load after expiry: %v", err)
		} else if _, cache, _ := sm.PeekCachedKey(); cache != nil {
			t.Fatalf("expired cache must be cleared, got %v", cache)
		}
	})
}

// TestClassifyAuthCause locks the shared vocabulary: every "re-enter password"
// condition maps to exactly one AuthRootCause, and a data-desync diagnosis maps
// to none (it must be reported, not papered over with a prompt).
func TestClassifyAuthCause(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want AuthRootCause
		ok   bool
	}{
		{"expired", ErrSessionExpired, AuthCauseExpired, true},
		{"restarted", fmt.Errorf("%w: restarted", ErrSessionInvalidated), AuthCauseRestarted, true},
		{"vault changed", fmt.Errorf("%w: %w", ErrSessionInvalidated, ErrSessionVaultChanged), AuthCauseVaultChanged, true},
		{"multiple cache", errMultipleSessionCaches, AuthCauseMultipleCache, true},
		{"unreadable", fmt.Errorf("%w: boom", ErrSessionUnverifiable), AuthCauseUnreadable, true},
		{"metadata replaced", ErrSessionStaleMetadata, AuthCauseMetadataReplaced, true},
		{"none", ErrNoSession, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyAuthCause(tc.err)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ClassifyAuthCause(%v) = (%q, %v), want (%q, %v)", tc.err, got, ok, tc.want, tc.ok)
			}
		})
	}
}
