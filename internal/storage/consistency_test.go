package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

// saveDesyncedEnv writes an env file that is encrypted with a different key,
// simulating a file pulled from another machine that was re-initialized.
func saveDesyncedEnv(t *testing.T, mgr *Manager, group string) {
	t.Helper()
	otherSalt, _ := crypto.GenerateSalt()
	otherKey := crypto.DeriveKey("other-password", otherSalt)
	enc, err := crypto.Encrypt(otherKey, []byte(`{"name":"x","variables":{}}`))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	name := "env_" + group + ".json.enc"
	if err := os.WriteFile(filepath.Join(mgr.dataPath, name), []byte(enc), 0o600); err != nil {
		t.Fatalf("write desync env: %v", err)
	}
}

func TestCheckConsistency_AllOK(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	// Add an env group so there is at least one data file to probe.
	grp := NewEnvGroup("prod")
	if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatalf("save env group: %v", err)
	}

	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if !report.MetadataKeyOK {
		t.Error("MetadataKeyOK should be true for the correct key")
	}
	if report.EnvFiles.OK != report.EnvFiles.Total || report.EnvFiles.Total == 0 {
		t.Errorf("env files should all decrypt; got %+v", report.EnvFiles)
	}
	if !report.AllOK() {
		t.Errorf("expected AllOK, got %+v", report)
	}
}

func TestCheckConsistency_PinpointsDesyncedFile(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	// Two consistent env groups...
	for _, g := range []string{"a", "b"} {
		grp := NewEnvGroup(g)
		if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
			t.Fatalf("save env group %s: %v", g, err)
		}
	}
	// ...plus one desynced group encrypted with a foreign key.
	saveDesyncedEnv(t, mgr, "rogue")

	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	// Initialize() creates a "default" group, so total = default + a + b + rogue.
	if report.EnvFiles.Total != 4 || report.EnvFiles.OK != 3 {
		t.Fatalf("expected 3/4 env files OK, got %+v", report.EnvFiles)
	}
	if len(report.EnvFiles.Failed) != 1 {
		t.Fatalf("expected exactly 1 failed env file, got %v", report.EnvFiles.Failed)
	}
	if report.EnvFiles.Failed[0] != "env_rogue.json.enc" {
		t.Errorf("expected env_rogue.json.enc in failed, got %s", report.EnvFiles.Failed[0])
	}
}

func TestCheckConsistency_NoPlaintextInReport(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	secret := "SUPER-SECRET-PLAINTEXT-MARKER"
	grp := NewEnvGroup("default")
	grp.Variables["canary"] = secret
	if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatalf("save env group: %v", err)
	}

	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	// The report must not carry plaintext anywhere in its rendered form.
	if strings.Contains(fmt.Sprintf("%+v", report), secret) {
		t.Errorf("report leaked plaintext: %+v", report)
	}
}

func TestCheckConsistency_WrongKeyLengthDoesNotPanic(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	grp := NewEnvGroup("default")
	if err := mgr.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatalf("save env group: %v", err)
	}

	for _, bad := range [][]byte{nil, make([]byte, 0), make([]byte, 31), make([]byte, 33)} {
		// Must not panic and must mark everything as failed.
		report, err := mgr.CheckConsistency(bad)
		if err != nil {
			t.Fatalf("CheckConsistency(bad key len %d): %v", len(bad), err)
		}
		if report.MetadataKeyOK {
			t.Errorf("wrong-length key must not decrypt metadata")
		}
		if report.EnvFiles.OK != 0 {
			t.Errorf("wrong-length key must decrypt nothing, got OK=%d", report.EnvFiles.OK)
		}
	}
}
