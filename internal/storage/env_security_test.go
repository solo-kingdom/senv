package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/securefs"
)

func TestStorageEnvValidation(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	invalidGroups := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "nul\x00group"}
	for _, group := range invalidGroups {
		t.Run("group/"+group, func(t *testing.T) {
			assertNoStorageAccessForInvalidEnv(t, m, func() error {
				return m.SaveEnvVarWithKey(group, "API_KEY", &EnvVarEntry{}, key)
			})
			assertNoStorageAccessForInvalidEnv(t, m, func() error {
				_, err := m.LoadEnvVarWithKey(group, "API_KEY", key)
				return err
			})
			assertNoStorageAccessForInvalidEnv(t, m, func() error { return m.DeleteEnvVar(group, "API_KEY") })
			assertNoStorageAccessForInvalidEnv(t, m, func() error {
				_, err := m.ListEnvVars(group)
				return err
			})
		})
	}
	invalidKeys := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "1KEY", "bad-key", "nul\x00key"}
	for _, envKey := range invalidKeys {
		t.Run("key/"+envKey, func(t *testing.T) {
			assertNoStorageAccessForInvalidEnv(t, m, func() error {
				return m.SaveEnvVarWithKey("default", envKey, &EnvVarEntry{}, key)
			})
			assertNoStorageAccessForInvalidEnv(t, m, func() error {
				_, err := m.LoadEnvVarWithKey("default", envKey, key)
				return err
			})
			assertNoStorageAccessForInvalidEnv(t, m, func() error { return m.DeleteEnvVar("default", envKey) })
		})
	}
}

func assertNoStorageAccessForInvalidEnv(t *testing.T, m *Manager, action func() error) {
	t.Helper()
	original := m.openRoot
	accessed := false
	m.openRoot = func(string) (securefs.TrustedRoot, error) {
		accessed = true
		return nil, fmt.Errorf("unexpected filesystem access")
	}
	defer func() { m.openRoot = original }()
	if err := action(); err == nil {
		t.Fatal("invalid env identity was accepted")
	}
	if accessed {
		t.Fatal("filesystem was accessed before env identity validation")
	}
}

func TestEnvTraversalSymlink(t *testing.T) {
	t.Run("parent", func(t *testing.T) {
		m, _ := setupTestManager(t)
		key := derivedKey(t, m, "test-password")
		envs := filepath.Join(m.dataPath, EnvDirName)
		if err := os.RemoveAll(envs); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		sentinel := filepath.Join(outside, "sentinel")
		if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, envs); err != nil {
			t.Fatal(err)
		}
		if err := m.SaveEnvVarWithKey("prod", "API_KEY", &EnvVarEntry{}, key); err == nil {
			t.Fatal("SaveEnvVarWithKey followed parent symlink")
		}
		assertStorageBytes(t, sentinel, "outside")
		if _, err := os.Lstat(filepath.Join(outside, "prod")); !os.IsNotExist(err) {
			t.Fatalf("outside group created through symlink: %v", err)
		}
	})

	t.Run("target", func(t *testing.T) {
		m, _ := setupTestManager(t)
		key := derivedKey(t, m, "test-password")
		outside := filepath.Join(t.TempDir(), "sentinel")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(m.dataPath, EnvDirName, "default", "API_KEY.enc")
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		if err := m.SaveEnvVarWithKey("default", "API_KEY", &EnvVarEntry{}, key); err == nil {
			t.Fatal("SaveEnvVarWithKey followed target symlink")
		}
		if err := m.DeleteEnvVar("default", "API_KEY"); err == nil {
			t.Fatal("DeleteEnvVar removed target symlink")
		}
		assertStorageBytes(t, outside, "outside")
	})
}
