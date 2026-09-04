package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func writeLegacyMixedIndex(t *testing.T, m *Manager) {
	t.Helper()
	index := &storage.ConfigIndex{Configs: map[string]storage.ConfigFile{
		"database-prod": {
			Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default",
		},
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := m.storage.GetConfigPath()
	if err := os.MkdirAll(cfgPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgPath, storage.ConfigIndexFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListWithWarningsSkipsQuarantinedLegacyEntry(t *testing.T) {
	m := newTestManager(t)
	writeLegacyMixedIndex(t, m)

	configs, warnings, err := m.ListWithWarnings("")
	if err != nil {
		t.Fatalf("ListWithWarnings failed on mixed index: %v", err)
	}
	if len(configs) != 1 || configs[0].Name != "database-prod" {
		t.Fatalf("configs = %+v, want only database-prod", configs)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want one", warnings)
	}
	if warnings[0].OldName != "feg:ai-ops-portal.pub" || !strings.Contains(warnings[0].Hint, "senv config repair") {
		t.Fatalf("warning = %+v, want old name and repair hint", warnings[0])
	}
}

func TestListFailsClosedOnQuarantinedLegacyEntry(t *testing.T) {
	m := newTestManager(t)
	writeLegacyMixedIndex(t, m)

	if _, err := m.List(""); err == nil {
		t.Fatal("plain List accepted quarantined legacy entry")
	}
	if _, err := m.Groups(); err == nil {
		t.Fatal("plain Groups accepted quarantined legacy entry")
	}
}

func TestGroupsWithWarningsSkipsQuarantinedLegacyEntry(t *testing.T) {
	m := newTestManager(t)
	writeLegacyMixedIndex(t, m)

	groups, warnings, err := m.GroupsWithWarnings()
	if err != nil {
		t.Fatalf("GroupsWithWarnings failed: %v", err)
	}
	if len(groups) != 1 || groups[0] != "default" {
		t.Fatalf("groups = %+v, want [default]", groups)
	}
	if len(warnings) != 1 || warnings[0].OldName != "feg:ai-ops-portal.pub" {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestListOnEmptyVaultWithoutIndexFile(t *testing.T) {
	m := newTestManager(t)
	if err := os.Remove(filepath.Join(m.storage.GetConfigPath(), storage.ConfigIndexFile)); err != nil {
		t.Fatal(err)
	}
	configs, err := m.List("")
	if err != nil {
		t.Fatalf("empty-vault list failed: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("configs = %+v, want empty", configs)
	}
}
