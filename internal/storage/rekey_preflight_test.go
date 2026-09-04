package storage

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

type rekeyTestInputs struct {
	key         []byte
	salt        string
	passwordKey string
}

func makeRekeyTestInputs(t *testing.T, password string) rekeyTestInputs {
	t.Helper()
	salt, err := crypto.GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := crypto.DeriveKeyWithIterations(password, salt, crypto.DefaultIterations)
	verifier, err := crypto.Encrypt(key, []byte(crypto.HashPassword(password)))
	if err != nil {
		t.Fatal(err)
	}
	return rekeyTestInputs{key: key, salt: base64.StdEncoding.EncodeToString(salt), passwordKey: verifier}
}

func TestRekeyPreflightEnumeratesAndDecryptsCompleteVault(t *testing.T) {
	manager, _ := setupTestManager(t)
	oldKey := derivedKey(t, manager, "test-password")
	if err := manager.SaveEnvVarWithKey("default", "TOKEN", &EnvVarEntry{Value: "secret"}, oldKey); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveTextFileWithKey("notes", "readme", NewTextEntry("text"), oldKey); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveConfigFileWithKey("app", []byte("config"), oldKey); err != nil {
		t.Fatal(err)
	}
	index, err := manager.LoadConfigIndex()
	if err != nil {
		t.Fatal(err)
	}
	index.Configs["app"] = ConfigFile{Name: "app", EncryptedFile: "app.enc", Group: ConfigDefaultGroup}
	if err := manager.SaveConfigIndex(index); err != nil {
		t.Fatal(err)
	}
	entries, result, metadata, err := manager.rekeyPreflight(oldKey)
	if err != nil {
		t.Fatalf("rekeyPreflight: %v", err)
	}
	if len(metadata) == 0 || len(entries) != result.Total() {
		t.Fatalf("entries=%d result=%+v metadata=%d", len(entries), result, len(metadata))
	}
	if result.EnvFiles != 2 || result.TextFiles != 1 || result.ConfigFiles != 1 {
		t.Fatalf("unexpected preflight counts: %+v", result)
	}
}

func TestRekeyNoMutationOnPreflightError(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*testing.T, *Manager, []byte)
	}{
		{
			name: "corrupt ciphertext",
			corrupt: func(t *testing.T, manager *Manager, _ []byte) {
				path := filepath.Join(manager.dataPath, EnvDirName, "default", EnvMetaFileName)
				if err := os.WriteFile(path, []byte("not ciphertext"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "corrupt config index JSON",
			corrupt: func(t *testing.T, manager *Manager, _ []byte) {
				if err := os.WriteFile(filepath.Join(manager.configPath, ConfigIndexFile), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "config index identity mismatch",
			corrupt: func(t *testing.T, manager *Manager, _ []byte) {
				index := &ConfigIndex{Configs: map[string]ConfigFile{"app": {Name: "other", EncryptedFile: "app.enc"}}}
				data, _ := ToJSON(index)
				if err := os.WriteFile(filepath.Join(manager.configPath, ConfigIndexFile), data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "missing indexed config ciphertext",
			corrupt: func(t *testing.T, manager *Manager, _ []byte) {
				index := &ConfigIndex{Configs: map[string]ConfigFile{"app": {Name: "app", EncryptedFile: "app.enc"}}}
				data, _ := ToJSON(index)
				if err := os.WriteFile(filepath.Join(manager.configPath, ConfigIndexFile), data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "invalid encrypted identity",
			corrupt: func(t *testing.T, manager *Manager, oldKey []byte) {
				dir := filepath.Join(manager.dataPath, TextDirName, "bad:group")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				ciphertext, _ := crypto.Encrypt(oldKey, []byte(`{"value":"x"}`))
				if err := os.WriteFile(filepath.Join(dir, "entry.enc"), []byte(ciphertext), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink traversal entry",
			corrupt: func(t *testing.T, manager *Manager, _ []byte) {
				target := filepath.Join(t.TempDir(), "outside")
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(manager.dataPath, "linked")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, _ := setupTestManager(t)
			oldKey := derivedKey(t, manager, "test-password")
			tc.corrupt(t, manager, oldKey)
			before := snapshotVault(t, manager)
			inputs := makeRekeyTestInputs(t, "new-password")
			if _, err := manager.Rekey(oldKey, inputs.key, inputs.salt, inputs.passwordKey, crypto.DefaultIterations); err == nil {
				t.Fatal("Rekey succeeded for corrupt preflight input")
			}
			after := snapshotVault(t, manager)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("vault changed on preflight failure\nbefore=%v\nafter=%v", sortedSnapshot(before), sortedSnapshot(after))
			}
			assertNoRekeyArtifacts(t, manager)
		})
	}
}

func snapshotVault(t *testing.T, manager *Manager) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, root := range []string{manager.configPath, manager.dataPath} {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				t.Fatalf("snapshot %s: %v", path, err)
			}
			if entry.IsDir() || entry.Name() == vaultMutationLockFile {
				return nil
			}
			rel, _ := filepath.Rel(filepath.Dir(manager.configPath), path)
			if entry.Type()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(path)
				result[rel] = "symlink:" + target
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("snapshot read %s: %v", path, readErr)
			}
			result[rel] = string(data)
			return nil
		})
	}
	return result
}

func sortedSnapshot(snapshot map[string]string) []string {
	lines := make([]string, 0, len(snapshot))
	for path, content := range snapshot {
		lines = append(lines, fmt.Sprintf("%s=%x", path, content))
	}
	sort.Strings(lines)
	return lines
}

func assertNoRekeyArtifacts(t *testing.T, manager *Manager) {
	t.Helper()
	for _, root := range []string{manager.configPath, manager.dataPath} {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(entry.Name(), ".rekey-") || entry.Name() == rekeyManifestFile {
				t.Errorf("rekey artifact remains: %s", path)
			}
			return nil
		})
	}
}
