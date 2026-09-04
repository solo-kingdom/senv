package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/securefs"
)

func TestStorageConfigValidation(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	invalid := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "nul\x00name"}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			assertNoStorageAccessForInvalidConfig(t, m, func() error {
				return m.SaveConfigFileWithKey(name, []byte("secret"), key)
			})
			assertNoStorageAccessForInvalidConfig(t, m, func() error {
				_, err := m.LoadConfigFileWithKey(name, key)
				return err
			})
			assertNoStorageAccessForInvalidConfig(t, m, func() error { return m.DeleteConfigFile(name) })
		})
	}
}

func assertNoStorageAccessForInvalidConfig(t *testing.T, m *Manager, action func() error) {
	t.Helper()
	original := m.openRoot
	accessed := false
	m.openRoot = func(string) (securefs.TrustedRoot, error) {
		accessed = true
		return nil, fmt.Errorf("unexpected filesystem access")
	}
	defer func() { m.openRoot = original }()
	if err := action(); err == nil {
		t.Fatal("invalid config identity was accepted")
	}
	if accessed {
		t.Fatal("filesystem was accessed before config identity validation")
	}
}

func TestConfigIndexValidation(t *testing.T) {
	tests := []struct {
		name     string
		entryKey string
		entry    ConfigFile
	}{
		{name: "map key", entryKey: "../app", entry: ConfigFile{Name: "../app", EncryptedFile: "../app.enc", Group: "default"}},
		{name: "Name mismatch", entryKey: "app", entry: ConfigFile{Name: "other", EncryptedFile: "app.enc", Group: "default"}},
		{name: "Group", entryKey: "app", entry: ConfigFile{Name: "app", EncryptedFile: "app.enc", Group: "../prod"}},
		{name: "EncryptedFile traversal", entryKey: "app", entry: ConfigFile{Name: "app", EncryptedFile: "../escaped.enc", Group: "default"}},
		{name: "EncryptedFile mismatch", entryKey: "app", entry: ConfigFile{Name: "app", EncryptedFile: "other.enc", Group: "default"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := setupTestManager(t)
			writeRawConfigIndex(t, m, &ConfigIndex{Configs: map[string]ConfigFile{tt.entryKey: tt.entry}})
			if _, err := m.LoadConfigIndex(); err == nil {
				t.Fatal("LoadConfigIndex accepted corrupt identity mapping")
			}
		})
	}
}

func TestConfigIndexTraversalDeleteFailsClosed(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	if err := m.SaveConfigFileWithKey("app", []byte("secret"), key); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	index := &ConfigIndex{Configs: map[string]ConfigFile{
		"app": {Name: "app", EncryptedFile: "../sentinel", Group: "default"},
	}}
	writeRawConfigIndex(t, m, index)
	if _, err := m.LoadConfigIndex(); err == nil {
		t.Fatal("corrupt index was accepted")
	}
	if _, err := os.Stat(filepath.Join(m.dataPath, "app.enc")); err != nil {
		t.Fatalf("managed ciphertext changed after corrupt index: %v", err)
	}
	assertStorageBytes(t, outside, "outside")
}

func TestConfigLegacyEmptyEncryptedFile(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	if err := m.SaveConfigFileWithKey("app", []byte("secret"), key); err != nil {
		t.Fatal(err)
	}
	legacy := &ConfigIndex{Configs: map[string]ConfigFile{
		"app": {Name: "app", EncryptedFile: "", Group: "default"},
	}}
	writeRawConfigIndex(t, m, legacy)
	before, err := os.ReadFile(filepath.Join(m.configPath, ConfigIndexFile))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := m.LoadConfigIndex()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Configs["app"].EncryptedFile != "app.enc" {
		t.Fatalf("legacy EncryptedFile normalized to %q", loaded.Configs["app"].EncryptedFile)
	}
	after, err := os.ReadFile(filepath.Join(m.configPath, ConfigIndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("read-only legacy normalization rewrote config index")
	}
	content, err := m.LoadConfigFileWithKey("app", key)
	if err != nil || string(content) != "secret" {
		t.Fatalf("legacy config load = %q, %v", content, err)
	}
}

func TestConfigTraversalSymlink(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(m.dataPath, "app.enc")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveConfigFileWithKey("app", []byte("secret"), key); err == nil {
		t.Fatal("SaveConfigFileWithKey followed target symlink")
	}
	if err := m.DeleteConfigFile("app"); err == nil {
		t.Fatal("DeleteConfigFile removed target symlink")
	}
	assertStorageBytes(t, outside, "outside")
}

func writeRawConfigIndex(t *testing.T, m *Manager, index *ConfigIndex) {
	t.Helper()
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.configPath, ConfigIndexFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
