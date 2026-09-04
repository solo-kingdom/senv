package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func setupLegacyConfigVault(t *testing.T) (cfg, data string) {
	t.Helper()
	dir := t.TempDir()
	cfg, data = newInitializedProject(t, dir, "correct-secret")
	useProjectPaths(t, cfg, data)
	authPrompt = stubPrompter("correct-secret")

	legacyName := "feg:ai-ops-portal.pub"
	if err := os.WriteFile(filepath.Join(data, legacyName+storage.ConfigFileSuffix), []byte("legacy-ciphertext"), 0o600); err != nil {
		t.Fatalf("write legacy ciphertext: %v", err)
	}
	index := &storage.ConfigIndex{Configs: map[string]storage.ConfigFile{
		legacyName: {
			Name:          legacyName,
			EncryptedFile: legacyName + storage.ConfigFileSuffix,
			Group:         "default",
			TargetPath:    "/tmp/portal.pub",
		},
	}}
	raw, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, storage.ConfigIndexFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg, data
}

func setRepairFlags(t *testing.T, dryRun, yes, dropMissing bool) {
	t.Helper()
	prevDry, prevYes, prevDrop := configRepairDryRun, configRepairYes, configRepairDropMissing
	configRepairDryRun, configRepairYes, configRepairDropMissing = dryRun, yes, dropMissing
	t.Cleanup(func() {
		configRepairDryRun, configRepairYes, configRepairDropMissing = prevDry, prevYes, prevDrop
	})
}

func TestConfigRepairCommandDryRun(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupLegacyConfigVault(t)
	setRepairFlags(t, true, false, false)

	var runErr error
	out := captureStdout(t, func() {
		runErr = configRepairCmd.RunE(configRepairCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("dry-run failed: %v", runErr)
	}
	if !strings.Contains(out, "feg:ai-ops-portal.pub -> feg_ai-ops-portal.pub") {
		t.Fatalf("plan missing from output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(data, "feg_ai-ops-portal.pub.enc")); !os.IsNotExist(err) {
		t.Fatalf("dry-run renamed ciphertext: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "feg:ai-ops-portal.pub.enc")); err != nil {
		t.Fatalf("dry-run removed legacy ciphertext: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, storage.ConfigIndexFile)); err != nil {
		t.Fatalf("dry-run removed index: %v", err)
	}
}

func TestConfigRepairCommandApply(t *testing.T) {
	isolateSessionCache(t)
	_, data := setupLegacyConfigVault(t)
	setRepairFlags(t, false, true, false)

	var runErr error
	out := captureStdout(t, func() {
		runErr = configRepairCmd.RunE(configRepairCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("apply failed: %v", runErr)
	}
	if !strings.Contains(out, "Config repair complete") {
		t.Fatalf("completion missing from output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(data, "feg_ai-ops-portal.pub.enc")); err != nil {
		t.Fatalf("ciphertext not renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "feg:ai-ops-portal.pub.enc")); !os.IsNotExist(err) {
		t.Fatalf("legacy ciphertext still present: %v", err)
	}

	// Post-repair list must succeed without quarantine warnings.
	mgr, err := getConfigManager()
	if err != nil {
		t.Fatalf("getConfigManager: %v", err)
	}
	configs, warnings, err := mgr.ListWithWarnings("")
	if err != nil {
		t.Fatalf("post-repair list failed: %v", err)
	}
	if len(configs) != 1 || configs[0].Name != "feg_ai-ops-portal.pub" {
		t.Fatalf("post-repair configs = %+v", configs)
	}
	if len(warnings) != 0 {
		t.Fatalf("post-repair warnings = %+v", warnings)
	}
}
