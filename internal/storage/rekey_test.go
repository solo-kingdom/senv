package storage

import (
	"encoding/base64"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func TestRekey_ReEncryptsAllFiles(t *testing.T) {
	mgr, _ := setupTestManager(t)
	oldKey := derivedKey(t, mgr, "test-password")

	grp := NewEnvGroup("prod")
	grp.Variables["API_KEY"] = "secret123"
	if err := mgr.SaveEnvGroupWithKey(grp, oldKey); err != nil {
		t.Fatalf("save env group: %v", err)
	}

	entry := NewTextEntry("some text value")
	if err := mgr.SaveTextFileWithKey("notes", "readme", entry, oldKey); err != nil {
		t.Fatalf("save text: %v", err)
	}

	newSalt, _ := crypto.GenerateSalt()
	newKey := crypto.DeriveKey("new-password", newSalt)
	newHash := crypto.HashPassword("new-password")
	newPasswordKey, _ := crypto.Encrypt(newKey, []byte(newHash))

	result, err := mgr.Rekey(oldKey, newKey,
		base64.StdEncoding.EncodeToString(newSalt), newPasswordKey)
	if err != nil {
		t.Fatalf("Rekey: %v", err)
	}

	// default group (.meta.enc) + prod group (.meta.enc + API_KEY.enc) = 3 env files
	if result.EnvFiles != 3 {
		t.Errorf("expected 3 env files, got %d", result.EnvFiles)
	}
	if result.TextFiles != 1 {
		t.Errorf("expected 1 text file, got %d", result.TextFiles)
	}

	// Old key should no longer work
	if _, err := mgr.LoadEnvGroupWithKey("prod", oldKey); err == nil {
		t.Error("old key should not decrypt after rekey")
	}

	// New key should work
	loaded, err := mgr.LoadEnvGroupWithKey("prod", newKey)
	if err != nil {
		t.Fatalf("load with new key: %v", err)
	}
	if loaded.Variables["API_KEY"] != "secret123" {
		t.Errorf("expected secret123, got %q", loaded.Variables["API_KEY"])
	}

	// Text should be readable with new key
	textEntry, err := mgr.LoadTextFileWithKey("notes", "readme", newKey)
	if err != nil {
		t.Fatalf("load text with new key: %v", err)
	}
	if textEntry.Value != "some text value" {
		t.Errorf("expected 'some text value', got %q", textEntry.Value)
	}

	// Password verification should work with new password
	if ok, err := mgr.VerifyPassword("new-password"); err != nil || !ok {
		t.Errorf("VerifyPassword should succeed with new password: ok=%v err=%v", ok, err)
	}
	if ok, _ := mgr.VerifyPassword("test-password"); ok {
		t.Error("VerifyPassword should fail with old password")
	}
}

func TestRekey_FailsWithWrongOldKey(t *testing.T) {
	mgr, _ := setupTestManager(t)
	correctKey := derivedKey(t, mgr, "test-password")

	grp := NewEnvGroup("prod")
	if err := mgr.SaveEnvGroupWithKey(grp, correctKey); err != nil {
		t.Fatalf("save env group: %v", err)
	}

	wrongKey := crypto.DeriveKey("wrong", []byte("0123456789abcdef0123456789abcdef"))
	newSalt, _ := crypto.GenerateSalt()
	newKey := crypto.DeriveKey("new-password", newSalt)
	newHash := crypto.HashPassword("new-password")
	newPasswordKey, _ := crypto.Encrypt(newKey, []byte(newHash))

	_, err := mgr.Rekey(wrongKey, newKey,
		base64.StdEncoding.EncodeToString(newSalt), newPasswordKey)
	if err == nil {
		t.Fatal("Rekey should fail with wrong old key")
	}

	// Original data should still be intact
	loaded, err := mgr.LoadEnvGroupWithKey("prod", correctKey)
	if err != nil {
		t.Fatalf("original data should be intact: %v", err)
	}
	if loaded.Name != "prod" {
		t.Errorf("expected group name 'prod', got %q", loaded.Name)
	}
}
