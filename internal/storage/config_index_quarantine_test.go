package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigIndexWithQuarantineSkipsLegacyColonName(t *testing.T) {
	m, _ := setupTestManager(t)
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"database-prod": {Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default"},
		"feg:ai-ops-portal.pub": {
			Name:          "feg:ai-ops-portal.pub",
			EncryptedFile: "feg:ai-ops-portal.pub.enc",
			Group:         "default",
			TargetPath:    "/tmp/portal.pub",
		},
	}})

	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		t.Fatalf("legacy colon name poisoned read-only load: %v", err)
	}
	if _, ok := index.Configs["database-prod"]; !ok {
		t.Fatal("valid entry was dropped alongside quarantined entry")
	}
	if len(quarantined) != 1 || quarantined[0].Name != "feg:ai-ops-portal.pub" {
		t.Fatalf("quarantine = %+v, want single feg:ai-ops-portal.pub", quarantined)
	}
	if quarantined[0].Record.EncryptedFile != "feg:ai-ops-portal.pub.enc" ||
		quarantined[0].Record.TargetPath != "/tmp/portal.pub" {
		t.Fatalf("quarantined record lost metadata: %+v", quarantined[0].Record)
	}
}

func TestLoadConfigIndexFailsClosedOnQuarantine(t *testing.T) {
	m, _ := setupTestManager(t)
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"feg:ai-ops-portal.pub": {
			Name:          "feg:ai-ops-portal.pub",
			EncryptedFile: "feg:ai-ops-portal.pub.enc",
			Group:         "default",
		},
	}})

	_, err := m.LoadConfigIndex()
	if err == nil {
		t.Fatal("LoadConfigIndex accepted quarantined legacy entry")
	}
	if !strings.Contains(err.Error(), "senv config repair") {
		t.Fatalf("error lacks repair guidance: %v", err)
	}
}

func TestLoadConfigIndexStructuralHazardsNeverQuarantined(t *testing.T) {
	tests := []struct {
		name      string
		entryKey  string
		entry     ConfigFile
		wantError string
	}{
		{
			name:      "traversal map key",
			entryKey:  "../escaped",
			entry:     ConfigFile{Name: "../escaped", EncryptedFile: "../escaped.enc", Group: "default"},
			wantError: "structural identity hazard",
		},
		{
			name:      "map key and Name mismatch",
			entryKey:  "app",
			entry:     ConfigFile{Name: "other", EncryptedFile: "app.enc", Group: "default"},
			wantError: "identity mismatch",
		},
		{
			name:      "EncryptedFile mismatch",
			entryKey:  "app",
			entry:     ConfigFile{Name: "app", EncryptedFile: "other.enc", Group: "default"},
			wantError: "encrypted file mismatch",
		},
		{
			name:      "drive-letter volume semantics",
			entryKey:  "C:evil",
			entry:     ConfigFile{Name: "C:evil", EncryptedFile: "C:evil.enc", Group: "default"},
			wantError: "structural identity hazard",
		},
		{
			name:      "traversal group",
			entryKey:  "app",
			entry:     ConfigFile{Name: "app", EncryptedFile: "app.enc", Group: "../prod"},
			wantError: "structural identity hazard",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := setupTestManager(t)
			writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{tt.entryKey: tt.entry}})
			if _, _, err := m.LoadConfigIndexWithQuarantine(); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("LoadConfigIndexWithQuarantine error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestConfigIndexMixedMatrix(t *testing.T) {
	m, _ := setupTestManager(t)
	// A structural hazard anywhere still fails the whole load, even when valid
	// and quarantineable entries are present.
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"database-prod": {Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default"},
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
		"../escaped": {Name: "../escaped", EncryptedFile: "../escaped.enc", Group: "default"},
	}})
	if _, _, err := m.LoadConfigIndexWithQuarantine(); err == nil {
		t.Fatal("structural hazard was quarantined instead of failing closed")
	}

	// Without the structural hazard the read-only view degrades gracefully.
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"database-prod": {Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default"},
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}})
	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		t.Fatalf("mixed valid+legacy load failed: %v", err)
	}
	if len(index.Configs) != 1 || len(quarantined) != 1 {
		t.Fatalf("index=%d quarantined=%d, want 1/1", len(index.Configs), len(quarantined))
	}
}

func TestLoadConfigIndexMissingFileIsEmptyIndex(t *testing.T) {
	m, _ := setupTestManager(t)
	if err := os.MkdirAll(m.configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	// setupTestManager initializes the vault with an empty index; removing it
	// simulates a vault that predates any config being stored.
	if err := os.Remove(filepath.Join(m.configPath, ConfigIndexFile)); err != nil {
		t.Fatal(err)
	}

	index, err := m.LoadConfigIndex()
	if err != nil {
		t.Fatalf("missing index treated as error: %v", err)
	}
	if index == nil || index.Configs == nil || len(index.Configs) != 0 {
		t.Fatalf("missing index returned %+v, want empty non-nil map", index)
	}
}

func TestLoadConfigIndexQuarantinesNonPortableGroupOnly(t *testing.T) {
	m, _ := setupTestManager(t)
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"app": {Name: "app", EncryptedFile: "app.enc", Group: "feg:prod"},
	}})
	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		t.Fatalf("non-portable group poisoned load: %v", err)
	}
	if len(index.Configs) != 0 || len(quarantined) != 1 {
		t.Fatalf("index=%d quarantined=%d, want 0/1", len(index.Configs), len(quarantined))
	}
	if quarantined[0].Reason != "non-portable legacy group" {
		t.Fatalf("reason = %q", quarantined[0].Reason)
	}
}

func TestCheckConsistencySkipsQuarantinedConfigs(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	// One decryptable valid config plus one quarantined legacy entry whose
	// ciphertext is deliberately missing: the quarantine path must not try to
	// open it, and the missing file must not surface as a probe failure.
	if err := m.SaveConfigFileWithKey("database-prod", []byte("secret"), key); err != nil {
		t.Fatal(err)
	}
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"database-prod": {Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default"},
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}})

	report, err := m.CheckConsistency(key)
	if err != nil {
		t.Fatalf("CheckConsistency failed on quarantined index: %v", err)
	}
	if report.ConfigFiles.Total != 1 || report.ConfigFiles.OK != 1 || len(report.ConfigFiles.Failed) != 0 {
		t.Fatalf("config probes = %+v, want 1/1 with no failures", report.ConfigFiles)
	}
	if len(report.QuarantinedConfigNames) != 1 || report.QuarantinedConfigNames[0] != "feg:ai-ops-portal.pub" {
		t.Fatalf("quarantined = %+v", report.QuarantinedConfigNames)
	}
	if !report.AllOK() {
		t.Fatal("quarantined entry must not make the consistency report NOT OK")
	}
}
