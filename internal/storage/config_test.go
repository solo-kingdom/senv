package storage

import (
	"encoding/base64"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func derivedKey(t *testing.T, mgr *Manager, password string) []byte {
	t.Helper()
	metadata, err := mgr.LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	return crypto.DeriveKey(password, salt)
}

func TestSaveAndLoadConfigFileWithKey(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	content := []byte("server: 8080\n")

	if err := mgr.SaveConfigFileWithKey("app", content, key); err != nil {
		t.Fatalf("SaveConfigFileWithKey: %v", err)
	}

	loaded, err := mgr.LoadConfigFileWithKey("app", key)
	if err != nil {
		t.Fatalf("LoadConfigFileWithKey: %v", err)
	}
	if string(loaded) != string(content) {
		t.Errorf("got %q, want %q", loaded, content)
	}
}

func TestConfigFilePasswordDelegatesToKey(t *testing.T) {
	mgr, _ := setupTestManager(t)
	content := []byte("host=localhost\n")

	if err := mgr.SaveConfigFile("db", content, "test-password"); err != nil {
		t.Fatalf("SaveConfigFile: %v", err)
	}

	key := derivedKey(t, mgr, "test-password")
	loaded, err := mgr.LoadConfigFileWithKey("db", key)
	if err != nil {
		t.Fatalf("LoadConfigFileWithKey after password save: %v", err)
	}
	if string(loaded) != string(content) {
		t.Errorf("got %q, want %q", loaded, content)
	}

	// Password load should also work for key-saved content.
	if err := mgr.SaveConfigFileWithKey("db2", content, key); err != nil {
		t.Fatalf("SaveConfigFileWithKey: %v", err)
	}
	loaded2, err := mgr.LoadConfigFile("db2", "test-password")
	if err != nil {
		t.Fatalf("LoadConfigFile after key save: %v", err)
	}
	if string(loaded2) != string(content) {
		t.Errorf("got %q, want %q", loaded2, content)
	}
}
