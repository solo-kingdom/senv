package cmd

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

func TestMigrateToV2CommandEndToEnd(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	dataPath := filepath.Join(dir, "data")
	store := storage.NewManager(cfgPath, dataPath)
	if err := os.MkdirAll(cfgPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataPath, storage.EnvDirName, "default"), 0o700); err != nil {
		t.Fatal(err)
	}

	// Build a legacy v1 vault (KeyEpoch=0, v1 ciphertexts, plaintext indexes).
	salt, _ := crypto.GenerateSalt()
	key := crypto.DeriveKeyWithIterations("vault-pw", salt, crypto.DefaultIterations)
	verifier, _ := crypto.Encrypt(key, []byte(crypto.HashPassword("vault-pw")))
	md := &storage.Metadata{
		Version:       "1.0",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Salt:          base64.StdEncoding.EncodeToString(salt),
		PasswordKey:   verifier,
		KDFIterations: crypto.DefaultIterations,
		KeyEpoch:      0,
	}
	if err := store.SaveMetadata(md); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(storage.NewSettings()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfigIndex(storage.NewConfigIndex()); err != nil {
		t.Fatal(err)
	}
	metaV1, _ := crypto.Encrypt(key, []byte(`{"name":"default"}`))
	_ = os.WriteFile(filepath.Join(dataPath, storage.EnvDirName, "default", storage.EnvMetaFileName), []byte(metaV1), 0o600)
	varV1, _ := crypto.Encrypt(key, []byte(`{"value":"legacy-token"}`))
	_ = os.WriteFile(filepath.Join(dataPath, storage.EnvDirName, "default", "API_TOKEN.enc"), []byte(varV1), 0o600)

	useProjectPaths(t, cfgPath, dataPath)
	authPrompt = stubPrompter("vault-pw")

	prevReq, prevNoReq, prevForce := migrateRequireV2, migrateNoRequireV2, migrateForceV2
	t.Cleanup(func() {
		migrateRequireV2, migrateNoRequireV2, migrateForceV2 = prevReq, prevNoReq, prevForce
	})
	migrateRequireV2 = true
	migrateNoRequireV2 = false
	migrateForceV2 = false

	var runErr error
	out := captureStdout(t, func() {
		runErr = runMigrateToV2(migrateToV2Cmd, nil)
	})
	if runErr != nil {
		t.Fatalf("runMigrateToV2: %v", runErr)
	}
	if !strings.Contains(out, "key_epoch=1") || !strings.Contains(out, "require_v2=true") {
		t.Fatalf("unexpected migrate to-v2 output:\n%s", out)
	}

	// Verify doctor output shows v2 envelope and OK indexes.
	var docOut strings.Builder
	if err := runDoctorAt(cfgPath, dataPath, stubPrompter("vault-pw"), &docOut); err != nil {
		t.Fatalf("runDoctorAt: %v", err)
	}
	if !strings.Contains(docOut.String(), "v2 (key_epoch=1, require_v2=true)") {
		t.Fatalf("doctor output missing v2 envelope line:\n%s", docOut.String())
	}

	// Verify config_index.json on disk is encrypted v2.
	rawIdx, _ := os.ReadFile(filepath.Join(cfgPath, storage.ConfigIndexFile))
	if !crypto.IsV2Ciphertext(string(rawIdx)) {
		t.Fatalf("config_index.json not encrypted after migrate to-v2: %q", rawIdx)
	}
}

func TestDecryptHistoryValueV2AndEncryptedIndex(t *testing.T) {
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}

	envCT, err := crypto.EncryptWithAAD(key, []byte(`{"value":"s3cr3t"}`), crypto.BuildEntryAAD("env", "prod", "TOKEN", 2))
	if err != nil {
		t.Fatal(err)
	}
	gotEnv, err := decryptHistoryValueWithEpoch(provider.HistoryVersion{
		Kind: "env", Grp: "prod", Key: "TOKEN", Ciphertext: []byte(envCT),
	}, key, 3)
	if err != nil || string(gotEnv) != `{"value":"s3cr3t"}` {
		t.Fatalf("decryptHistoryValueWithEpoch env = (%q, %v)", gotEnv, err)
	}

	idxCT, err := crypto.EncryptWithAAD(key, []byte(`{"configs":{}}`), crypto.BuildEntryAAD("config_index", "", "", 1))
	if err != nil {
		t.Fatal(err)
	}
	gotIdx, err := decryptHistoryValueWithEpoch(provider.HistoryVersion{
		Kind: "config_index", Ciphertext: []byte(idxCT),
	}, key, 2)
	if err != nil || string(gotIdx) != `{"configs":{}}` {
		t.Fatalf("decryptHistoryValueWithEpoch config_index = (%q, %v)", gotIdx, err)
	}

	// Wrong identity must fail AAD check.
	if _, err := decryptHistoryValueWithEpoch(provider.HistoryVersion{
		Kind: "env", Grp: "prod", Key: "OTHER", Ciphertext: []byte(envCT),
	}, key, 3); err == nil {
		t.Fatal("expected AAD mismatch failure for wrong history key")
	}
}
