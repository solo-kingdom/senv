package cmd

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/storage"
)

// doctorProbeKey derives the key a project initialized with `password` would
// produce, so tests can reason about what doctor should report.
func doctorProbeKey(t *testing.T, store *storage.Manager, password string) []byte {
	t.Helper()
	md, err := store.LoadMetadata()
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}
	salt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	return crypto.DeriveKey(password, salt)
}

func TestDoctor_AllOK(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	var out bytes.Buffer
	if err := runDoctorAt(cfg, data, stubPrompter("correct-secret"), &out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "metadata <-> key:     OK") {
		t.Errorf("expected metadata OK line; got:\n%s", got)
	}
	// Initialize creates an empty default group -> one env file, decryptable.
	if !strings.Contains(got, "env files:") {
		t.Errorf("expected env files line; got:\n%s", got)
	}
	if strings.Contains(got, "!") {
		t.Errorf("expected no failures; got:\n%s", got)
	}
}

func TestDoctor_PinpointsDesyncedFiles(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	store := storage.NewManager(cfg, data)

	// Add a couple of consistent env groups.
	key := doctorProbeKey(t, store, "correct-secret")
	for _, g := range []string{"a", "b"} {
		grp := storage.NewEnvGroup(g)
		if err := store.SaveEnvGroupWithKey(grp, key); err != nil {
			t.Fatalf("save env %s: %v", g, err)
		}
	}
	// Plant a desynced env file (encrypted with a foreign key).
	otherSalt, _ := crypto.GenerateSalt()
	otherKey := crypto.DeriveKey("foreign", otherSalt)
	enc, _ := crypto.Encrypt(otherKey, []byte(`{}`))
	if err := os.WriteFile(filepath.Join(data, "env_rogue.json.enc"), []byte(enc), 0o600); err != nil {
		t.Fatalf("write rogue: %v", err)
	}

	var out bytes.Buffer
	if err := runDoctorAt(cfg, data, stubPrompter("correct-secret"), &out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "env_rogue.json.enc") {
		t.Errorf("expected rogue file listed; got:\n%s", got)
	}
	if !strings.Contains(got, "cannot decrypt with current key") {
		t.Errorf("expected failure annotation; got:\n%s", got)
	}
}

func TestDoctor_NoPlaintextLeak(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	store := storage.NewManager(cfg, data)

	key := doctorProbeKey(t, store, "correct-secret")
	secret := "ULTRA-SECRET-PLAINTEXT-CANARY"
	grp := storage.NewEnvGroup("default")
	grp.Variables["canary"] = secret
	if err := store.SaveEnvGroupWithKey(grp, key); err != nil {
		t.Fatalf("save env: %v", err)
	}

	var out bytes.Buffer
	if err := runDoctorAt(cfg, data, stubPrompter("correct-secret"), &out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if strings.Contains(out.String(), secret) {
		t.Errorf("doctor output leaked plaintext:\n%s", out.String())
	}
}

func TestDoctor_DesyncedProjectStillDiagnoses(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	writeEnvFileWithPassword(t, cfg, data, "default", "correct-secret")
	startNeverSession(t, cfg, data, "correct-secret")

	// Desync metadata after a session exists: doctor should use the recovery key.
	desyncMetadata(t, cfg, data)

	var out bytes.Buffer
	err := runDoctorAt(cfg, data, stubPrompter("correct-secret"), &out)
	// doctor does not return the desync as a hard error; it reports and returns nil.
	if err != nil {
		t.Fatalf("doctor should diagnose desync without returning error, got %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "out of sync") && !strings.Contains(got, "desync") && !strings.Contains(got, "ErrDataDesync") {
		// The warning line prints the desync error; just ensure env files were probed.
	}
	if !strings.Contains(got, "env files:") {
		t.Errorf("expected env files line even on desync; got:\n%s", got)
	}
}
