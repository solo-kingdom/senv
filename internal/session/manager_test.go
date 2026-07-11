package session

import (
	"encoding/base64"
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

	timeout, err := ParseTimeout("never")
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
		TimeoutType:  string(TimeoutNever),
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

	timeout, err := ParseTimeout("never")
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

	timeout, err := ParseTimeout("never")
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
