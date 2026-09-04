package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func writeLegacyCiphertext(t *testing.T, m *Manager, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.dataPath, name+ConfigFileSuffix), []byte("legacy-ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPlanConfigRepairSuggestsPortableName(t *testing.T) {
	m, _ := setupTestManager(t)
	writeLegacyCiphertext(t, m, "feg:ai-ops-portal.pub")
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
			TargetPath: "/tmp/portal.pub",
		},
	}})

	items, err := m.PlanConfigRepair()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v, want one", items)
	}
	it := items[0]
	if it.OldName != "feg:ai-ops-portal.pub" || it.NewName != "feg_ai-ops-portal.pub" {
		t.Fatalf("plan = %+v", it)
	}
	if it.MissingFile {
		t.Fatal("existing ciphertext reported missing")
	}
	if it.Record.TargetPath != "/tmp/portal.pub" {
		t.Fatalf("plan lost metadata: %+v", it.Record)
	}
}

func TestPlanConfigRepairRejectsSuggestionConflict(t *testing.T) {
	m, _ := setupTestManager(t)
	writeLegacyCiphertext(t, m, "feg:ai-ops-portal.pub")
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"database-prod":         {Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default"},
		"feg:ai-ops-portal.pub": {Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default"},
		"feg_ai-ops-portal.pub": {Name: "feg_ai-ops-portal.pub", EncryptedFile: "feg_ai-ops-portal.pub.enc", Group: "default"},
	}})

	if _, err := m.PlanConfigRepair(); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestPlanConfigRepairMarksMissingCiphertext(t *testing.T) {
	m, _ := setupTestManager(t)
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}})

	items, err := m.PlanConfigRepair()
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].MissingFile {
		t.Fatal("missing ciphertext not detected")
	}
}

func TestExecuteConfigRepairRenamesFileAndIndex(t *testing.T) {
	m, _ := setupTestManager(t)
	writeLegacyCiphertext(t, m, "feg:ai-ops-portal.pub")
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"database-prod": {Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default"},
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
			TargetPath: "/tmp/portal.pub",
		},
	}})

	err := m.ExecuteConfigRepair(map[string]string{"feg:ai-ops-portal.pub": "feg_ai-ops-portal.pub"}, false)
	if err != nil {
		t.Fatal(err)
	}

	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantine not cleared: %+v", quarantined)
	}
	fixed, ok := index.Configs["feg_ai-ops-portal.pub"]
	if !ok || fixed.EncryptedFile != "feg_ai-ops-portal.pub.enc" || fixed.TargetPath != "/tmp/portal.pub" {
		t.Fatalf("repaired entry = %+v", fixed)
	}
	if _, err := os.Stat(filepath.Join(m.dataPath, "feg_ai-ops-portal.pub.enc")); err != nil {
		t.Fatalf("ciphertext not renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.dataPath, "feg:ai-ops-portal.pub.enc")); !os.IsNotExist(err) {
		t.Fatalf("old ciphertext still present: %v", err)
	}
}

func TestExecuteConfigRepairPreservesDecryptability(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	encrypted, err := crypto.Encrypt(key, []byte("portal secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.dataPath, "feg:ai-ops-portal.pub.enc"), []byte(encrypted), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}})

	if err := m.ExecuteConfigRepair(map[string]string{"feg:ai-ops-portal.pub": "feg_ai-ops-portal.pub"}, false); err != nil {
		t.Fatal(err)
	}
	plaintext, err := m.LoadConfigFileWithKey("feg_ai-ops-portal.pub", key)
	if err != nil {
		t.Fatalf("post-repair load: %v", err)
	}
	if string(plaintext) != "portal secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestExecuteConfigRepairMissingFileRequiresDrop(t *testing.T) {
	m, _ := setupTestManager(t)
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}})

	err := m.ExecuteConfigRepair(map[string]string{"feg:ai-ops-portal.pub": "feg_ai-ops-portal.pub"}, false)
	if err == nil || !strings.Contains(err.Error(), "--drop-missing") {
		t.Fatalf("missing-file rename error = %v", err)
	}
	if _, qerr := m.LoadConfigIndex(); qerr == nil {
		t.Fatal("index was modified despite missing ciphertext")
	}

	if err := m.ExecuteConfigRepair(nil, true); err != nil {
		t.Fatal(err)
	}
	index, err := m.LoadConfigIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Configs) != 0 {
		t.Fatalf("stale entry not dropped: %+v", index.Configs)
	}
}

func TestExecuteConfigRepairRejectsUndecidedQuarantine(t *testing.T) {
	m, _ := setupTestManager(t)
	writeLegacyCiphertext(t, m, "feg:ai-ops-portal.pub")
	writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}})

	err := m.ExecuteConfigRepair(nil, false)
	if err == nil || !strings.Contains(err.Error(), "no repair decision") {
		t.Fatalf("undecided quarantine error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.dataPath, "feg:ai-ops-portal.pub.enc")); err != nil {
		t.Fatalf("ciphertext changed on refused repair: %v", err)
	}
}
