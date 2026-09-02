package storage

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

// TestLegacyMetadataUnlock pins backward compatibility: a vault whose
// metadata.json predates kdf_iterations (implicit 100000 iterations) must
// still unlock and decrypt data.
func TestLegacyMetadataUnlock(t *testing.T) {
	mgr, _ := setupTestManager(t)
	const password = "test-password"

	// Derive the key with the new default (what the manager wrote), then
	// downgrade the metadata on disk to legacy form: re-derive everything
	// with 100000 iterations and strip the kdf_iterations field.
	md, err := mgr.LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	if md.EffectiveIterations() != crypto.DefaultIterations {
		t.Fatalf("precondition: fresh vault should use %d iterations", crypto.DefaultIterations)
	}
	newSalt, _ := crypto.GenerateSalt()
	key := crypto.DeriveKeyWithIterations(password, newSalt, crypto.LegacyIterations)
	passwordHash := crypto.HashPassword(password)
	passwordKey, err := crypto.Encrypt(key, []byte(passwordHash))
	if err != nil {
		t.Fatalf("encrypt password key: %v", err)
	}

	legacy := map[string]interface{}{
		"version":      "1.0",
		"salt":         base64.StdEncoding.EncodeToString(newSalt),
		"password_key": passwordKey,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy metadata: %v", err)
	}
	if err := WriteSensitiveFile(mgr.GetConfigPath()+"/"+MetadataFile, data, 0o700, 0o600); err != nil {
		t.Fatalf("write legacy metadata: %v", err)
	}

	// Unlock via password must work against legacy metadata.
	valid, err := mgr.VerifyPassword(password)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !valid {
		t.Fatal("legacy metadata (100k iterations) should unlock with correct password")
	}

	// Data round-trip with the legacy-derived key must work.
	grp := NewEnvGroup("legacy")
	grp.Variables["TOKEN"] = "legacy-secret"
	if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatalf("save env group: %v", err)
	}
	loaded, err := mgr.LoadEnvGroup("legacy", password)
	if err != nil {
		t.Fatalf("load env group with password: %v", err)
	}
	if loaded.Variables["TOKEN"] != "legacy-secret" {
		t.Errorf("got %q, want %q", loaded.Variables["TOKEN"], "legacy-secret")
	}
}
