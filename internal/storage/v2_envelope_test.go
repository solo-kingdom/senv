package storage

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
)

func TestV2CrossEntryAADTransplantRejected(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	// Populate distinct entry kinds and names.
	kp := &KeyPairEntry{Name: "prod", PrivateKey: "PRIVATE-KEY-MATERIAL"}
	if err := mgr.SaveKeyPairWithKey("prod", kp, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveConfigFileWithKey("prod", []byte("orig-config"), key); err != nil {
		t.Fatal(err)
	}
	idx, err := mgr.LoadConfigIndexWithKey(key)
	if err != nil {
		t.Fatal(err)
	}
	idx.Configs["prod"] = ConfigFile{Name: "prod", EncryptedFile: "prod.enc", Group: ConfigDefaultGroup}
	if err := mgr.SaveConfigIndexWithKey(idx, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveEnvVarWithKey("default", "SECRET_A", &EnvVarEntry{Value: "val-a"}, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveEnvVarWithKey("default", "SECRET_B", &EnvVarEntry{Value: "val-b"}, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveEnvGroup(NewEnvGroup("other"), "test-password"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveEnvVarWithKey("other", "SECRET_A", &EnvVarEntry{Value: "val-other"}, key); err != nil {
		t.Fatal(err)
	}

	kpCT, err := os.ReadFile(mgr.keypairFilePath("prod"))
	if err != nil {
		t.Fatal(err)
	}
	if !crypto.IsV2Ciphertext(string(kpCT)) {
		t.Fatalf("expected v2 ciphertext for keypair, got %q", kpCT)
	}

	// 1. Transplant ssh_keypair("prod") ciphertext into config("prod") -> MUST fail.
	cfgPath := filepath.Join(mgr.GetDataPath(), "prod.enc")
	if err := os.WriteFile(cfgPath, kpCT, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadConfigFileWithKey("prod", key); !errors.Is(err, crypto.ErrDecryptionFailed) {
		t.Fatalf("transplanted keypair->config decrypted or wrong err: %v", err)
	}

	// 2. Transplant env(default/SECRET_A) -> env(default/SECRET_B) -> MUST fail.
	envACT, err := os.ReadFile(mgr.envVarPath("default", "SECRET_A"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.envVarPath("default", "SECRET_B"), envACT, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadEnvVarWithKey("default", "SECRET_B", key); !errors.Is(err, crypto.ErrDecryptionFailed) {
		t.Fatalf("swapped env A->B err = %v, want ErrDecryptionFailed", err)
	}

	// 3. Transplant env(default/SECRET_A) -> env(other/SECRET_A) -> MUST fail.
	if err := os.WriteFile(mgr.envVarPath("other", "SECRET_A"), envACT, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadEnvVarWithKey("other", "SECRET_A", key); !errors.Is(err, crypto.ErrDecryptionFailed) {
		t.Fatalf("cross-group env err = %v, want ErrDecryptionFailed", err)
	}

	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.AllOK() {
		t.Fatal("CheckConsistency must report failure on transplanted ciphertexts")
	}
}

func TestV2RenameReencryptsAADAndUnkeyedFailsClosed(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	if err := mgr.SaveEnvVarWithKey("default", "OLD_K", &EnvVarEntry{Value: "v1"}, key); err != nil {
		t.Fatal(err)
	}
	oldEnvCT, _ := os.ReadFile(mgr.envVarPath("default", "OLD_K"))

	// Unkeyed legacy rename must fail closed on v2 ciphertext.
	if err := mgr.RenameEnvVar("default", "OLD_K", "NEW_K"); !errors.Is(err, ErrEncryptedRenameRequiresKey) {
		t.Fatalf("unkeyed RenameEnvVar err = %v, want ErrEncryptedRenameRequiresKey", err)
	}

	// Keyed rename re-encrypts with new AAD.
	if err := mgr.RenameEnvVarWithKey("default", "OLD_K", "NEW_K", key); err != nil {
		t.Fatalf("RenameEnvVarWithKey: %v", err)
	}
	got, err := mgr.LoadEnvVarWithKey("default", "NEW_K", key)
	if err != nil || got.Value != "v1" {
		t.Fatalf("LoadEnvVarWithKey after rename = (%+v, %v)", got, err)
	}

	// Group rename re-encrypts meta + vars.
	if err := mgr.SaveEnvGroup(NewEnvGroup("g1"), "test-password"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveEnvVarWithKey("g1", "ITEM", &EnvVarEntry{Value: "g1-val"}, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenameEnvGroupWithKey("g1", "g2", key); err != nil {
		t.Fatalf("RenameEnvGroupWithKey: %v", err)
	}
	g2Val, err := mgr.LoadEnvVarWithKey("g2", "ITEM", key)
	if err != nil || g2Val.Value != "g1-val" {
		t.Fatalf("LoadEnvVarWithKey after group rename = (%+v, %v)", g2Val, err)
	}

	// Text & backup renames.
	if err := mgr.SaveTextFileWithKey("default", "t1", NewTextEntry("txt"), key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenameTextFileWithKey("default", "t1", "t2", key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenameTextGroupWithKey("default", "docs", key); err != nil {
		t.Fatal(err)
	}
	if txt, err := mgr.LoadTextFileWithKey("docs", "t2", key); err != nil || txt.Value != "txt" {
		t.Fatalf("text after rename = (%+v, %v)", txt, err)
	}

	if err := mgr.SaveBackupFileWithKey("default", "b1", NewBackupEntry("bak"), key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenameBackupFileWithKey("default", "b1", "b2", key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenameBackupGroupWithKey("default", "baks", key); err != nil {
		t.Fatal(err)
	}
	if bak, err := mgr.LoadBackupFileWithKey("baks", "b2", key); err != nil || bak.Value != "bak" {
		t.Fatalf("backup after rename = (%+v, %v)", bak, err)
	}

	// Config rename.
	if err := mgr.SaveConfigFileWithKey("c1", []byte("cfg-body"), key); err != nil {
		t.Fatal(err)
	}
	idx, err := mgr.LoadConfigIndexWithKey(key)
	if err != nil {
		t.Fatal(err)
	}
	idx.Configs["c1"] = ConfigFile{Name: "c1", EncryptedFile: "c1.enc", Group: ConfigDefaultGroup}
	if err := mgr.SaveConfigIndexWithKey(idx, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenameConfigFileWithKey("c1", "c2", key); err != nil {
		t.Fatalf("RenameConfigFileWithKey: %v", err)
	}
	if body, err := mgr.LoadConfigFileWithKey("c2", key); err != nil || string(body) != "cfg-body" {
		t.Fatalf("config after rename = (%q, %v)", body, err)
	}

	report, err := mgr.CheckConsistency(key)
	if err != nil || !report.AllOK() {
		t.Fatalf("CheckConsistency after renames: err=%v report=%+v", err, report)
	}

	// Copying old ciphertext (old AAD) to new path must fail.
	if err := os.WriteFile(mgr.envVarPath("default", "NEW_K"), oldEnvCT, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadEnvVarWithKey("default", "NEW_K", key); !errors.Is(err, crypto.ErrDecryptionFailed) {
		t.Fatalf("replayed old ciphertext err = %v, want ErrDecryptionFailed", err)
	}
}

func TestEncryptedIndexesAndRequireV2AntiDowngrade(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	cfgIdx := NewConfigIndex()
	cfgIdx.Configs["secret-app"] = ConfigFile{
		Name:          "secret-app",
		EncryptedFile: "secret-app.enc",
		TargetPath:    "/home/alice/.secret-app.conf",
		Description:   "sensitive-target-path-description",
	}
	if err := mgr.SaveConfigIndexWithKey(cfgIdx, key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveConfigFileWithKey("secret-app", []byte("cfg"), key); err != nil {
		t.Fatal(err)
	}
	skillIdx := NewSkillIndex()
	skillIdx.Skills["internal-skill"] = SkillEntry{
		ID:            "internal-skill",
		Group:         "work",
		Description:   "confidential-skill-desc",
		EncryptedFile: SkillEncryptedFile("work", "internal-skill"),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := mgr.SaveSkillIndexWithKey(skillIdx, key); err != nil {
		t.Fatal(err)
	}

	// Verify on-disk files are v2 envelopes with zero plaintext leakage.
	rawCfg, _ := os.ReadFile(filepath.Join(mgr.GetConfigPath(), ConfigIndexFile))
	if !crypto.IsV2Ciphertext(string(rawCfg)) || strings.Contains(string(rawCfg), "secret-app") || strings.Contains(string(rawCfg), "sensitive-target-path") {
		t.Fatalf("config_index.json leaked plaintext or is not v2: %q", rawCfg)
	}
	rawSkill, _ := os.ReadFile(filepath.Join(mgr.GetDataPath(), SkillIndexFile))
	if !crypto.IsV2Ciphertext(string(rawSkill)) || strings.Contains(string(rawSkill), "internal-skill") || strings.Contains(string(rawSkill), "confidential-skill-desc") {
		t.Fatalf("skill_index.json leaked plaintext or is not v2: %q", rawSkill)
	}

	// Unkeyed load fails closed.
	if _, err := mgr.LoadConfigIndex(); !errors.Is(err, ErrEncryptedIndexRequiresKey) {
		t.Fatalf("unkeyed LoadConfigIndex err = %v, want ErrEncryptedIndexRequiresKey", err)
	}
	if _, err := mgr.LoadSkillIndex(); !errors.Is(err, ErrEncryptedIndexRequiresKey) {
		t.Fatalf("unkeyed LoadSkillIndex err = %v, want ErrEncryptedIndexRequiresKey", err)
	}

	// Enable strict require_v2 policy.
	settings, err := mgr.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Vault.RequireV2 = true
	if err := mgr.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	// Downgrading an env entry to valid v1 ciphertext under the SAME vault key must fail.
	v1Payload, _ := ToJSON(&EnvVarEntry{Value: "downgraded"})
	v1CT, _ := crypto.Encrypt(key, v1Payload)
	if err := os.WriteFile(mgr.envVarPath("default", "V1_DOWNGRADE"), []byte(v1CT), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadEnvVarWithKey("default", "V1_DOWNGRADE", key); !errors.Is(err, crypto.ErrV1CiphertextRejected) {
		t.Fatalf("require_v2 v1 entry err = %v, want ErrV1CiphertextRejected", err)
	}
	_ = mgr.DeleteEnvVar("default", "V1_DOWNGRADE")

	// Downgrading config_index.json to plaintext JSON must fail.
	plainIdx, _ := ToJSON(NewConfigIndex())
	if err := os.WriteFile(filepath.Join(mgr.GetConfigPath(), ConfigIndexFile), plainIdx, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadConfigIndexWithKey(key); !errors.Is(err, ErrPlaintextIndexRejected) {
		t.Fatalf("require_v2 plaintext config_index err = %v, want ErrPlaintextIndexRejected", err)
	}
	if err := mgr.SaveConfigIndex(NewConfigIndex()); !errors.Is(err, ErrPlaintextIndexRejected) {
		t.Fatalf("require_v2 unkeyed SaveConfigIndex err = %v, want ErrPlaintextIndexRejected", err)
	}

	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.AllOK() {
		t.Fatal("CheckConsistency must fail when plaintext index violates require_v2")
	}
}

func TestV1VaultUpgradeAndEpochReplayRejected(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "config"), filepath.Join(dir, "data"))
	if err := os.MkdirAll(mgr.GetConfigPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mgr.GetDataPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	salt, _ := crypto.GenerateSalt()
	key := crypto.DeriveKeyWithIterations("pw1", salt, crypto.DefaultIterations)
	verifier, _ := crypto.Encrypt(key, []byte(crypto.HashPassword("pw1")))
	md := &Metadata{
		Version:       currentMetadataVersion,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Salt:          base64.StdEncoding.EncodeToString(salt),
		PasswordKey:   verifier,
		KDFIterations: crypto.DefaultIterations,
		KeyEpoch:      0,
	}
	if err := mgr.SaveMetadata(md); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveSettings(NewSettings()); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveConfigIndex(NewConfigIndex()); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveSkillIndex(NewSkillIndex()); err != nil {
		t.Fatal(err)
	}
	// Write a legacy v1 env var directly.
	if err := os.MkdirAll(mgr.envGroupDir("default"), 0o700); err != nil {
		t.Fatal(err)
	}
	metaV1, _ := crypto.Encrypt(key, []byte(`{"name":"default"}`))
	_ = os.WriteFile(mgr.envMetaPath("default"), []byte(metaV1), 0o600)
	varV1, _ := crypto.Encrypt(key, []byte(`{"value":"legacy-v1"}`))
	_ = os.WriteFile(mgr.envVarPath("default", "LEGACY"), []byte(varV1), 0o600)

	preReport, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatal(err)
	}
	if !preReport.AllOK() || preReport.LegacyV1Count != 4 {
		t.Fatalf("preReport = %+v, want AllOK=true LegacyV1Count=4", preReport)
	}

	// Rekey to v2 epoch=1 (even with same password / new salt).
	salt2, _ := crypto.GenerateSalt()
	key2 := crypto.DeriveKeyWithIterations("pw1", salt2, crypto.DefaultIterations)
	verifier2, _ := crypto.EncryptWithAAD(key2, []byte(crypto.HashPassword("pw1")), metadataVerifierAAD(1))
	res, err := mgr.Rekey(key, key2, base64.StdEncoding.EncodeToString(salt2), verifier2, crypto.DefaultIterations)
	if err != nil {
		t.Fatalf("Rekey v1->v2: %v", err)
	}
	if res.IndexFiles != 2 {
		t.Fatalf("IndexFiles = %d, want 2", res.IndexFiles)
	}
	mdAfter, _ := mgr.LoadMetadata()
	if mdAfter.KeyEpoch != 1 {
		t.Fatalf("KeyEpoch = %d, want 1", mdAfter.KeyEpoch)
	}
	epoch1EnvCT, _ := os.ReadFile(mgr.envVarPath("default", "LEGACY"))
	if !crypto.IsV2Ciphertext(string(epoch1EnvCT)) {
		t.Fatalf("expected v2 ciphertext after rekey, got %q", epoch1EnvCT)
	}

	// Rekey again to epoch=2 with same key2 (simulating same-key epoch bump) -> replaying epoch=1 ciphertext MUST fail!
	verifier3, _ := crypto.EncryptWithAAD(key2, []byte(crypto.HashPassword("pw1")), metadataVerifierAAD(2))
	if _, err := mgr.Rekey(key2, key2, base64.StdEncoding.EncodeToString(salt2), verifier3, crypto.DefaultIterations); err != nil {
		t.Fatalf("Rekey epoch 1->2: %v", err)
	}
	if err := os.WriteFile(mgr.envVarPath("default", "LEGACY"), epoch1EnvCT, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadEnvVarWithKey("default", "LEGACY", key2); !errors.Is(err, crypto.ErrDecryptionFailed) {
		t.Fatalf("replayed epoch-1 ciphertext in epoch-2 vault err = %v, want ErrDecryptionFailed", err)
	}
}
