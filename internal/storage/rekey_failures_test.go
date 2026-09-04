package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

var errInjectedRekey = errors.New("injected rekey failure")

func setupRekeyVault(t *testing.T) (*Manager, []byte) {
	t.Helper()
	manager, _ := setupTestManager(t)
	oldKey := derivedKey(t, manager, "test-password")
	if err := manager.SaveEnvVarWithKey("default", "TOKEN", &EnvVarEntry{Value: "env-secret"}, oldKey); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveTextFileWithKey("notes", "readme", NewTextEntry("text-secret"), oldKey); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveConfigFileWithKey("app", []byte("config-secret"), oldKey); err != nil {
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
	return manager, oldKey
}

func assertVaultDecryptsWithKey(t *testing.T, manager *Manager, key []byte) {
	t.Helper()
	env, err := manager.LoadEnvVarWithKey("default", "TOKEN", key)
	if err != nil || env.Value != "env-secret" {
		t.Fatalf("env decrypt: entry=%#v err=%v", env, err)
	}
	text, err := manager.LoadTextFileWithKey("notes", "readme", key)
	if err != nil || text.Value != "text-secret" {
		t.Fatalf("text decrypt: entry=%#v err=%v", text, err)
	}
	config, err := manager.LoadConfigFileWithKey("app", key)
	if err != nil || string(config) != "config-secret" {
		t.Fatalf("config decrypt: content=%q err=%v", config, err)
	}
}

func TestRekeyPrepareFailures(t *testing.T) {
	cases := []struct {
		operation string
		index     int
	}{
		{operation: "encrypt", index: 0},
		{operation: "manifest_PREPARE", index: 0},
		{operation: "write_metadata_new", index: 0},
		{operation: "write_new", index: 0},
		{operation: "new_file_fsync", index: 0},
		{operation: "new_dir_fsync", index: 0},
		{operation: "manifest_SWITCH_DATA", index: 0},
	}
	for _, tc := range cases {
		t.Run(tc.operation, func(t *testing.T) {
			manager, oldKey := setupRekeyVault(t)
			before := snapshotVault(t, manager)
			manager.rekeyHooks = &rekeyHooks{before: func(operation string, index int) error {
				if operation == tc.operation && index == tc.index {
					return errInjectedRekey
				}
				return nil
			}}
			inputs := makeRekeyTestInputs(t, "new-password")
			if _, err := manager.Rekey(oldKey, inputs.key, inputs.salt, inputs.passwordKey, crypto.DefaultIterations); err == nil {
				t.Fatal("Rekey succeeded at injected PREPARE failure")
			}
			manager.rekeyHooks = nil
			after := snapshotVault(t, manager)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("vault changed after prepare failure\nbefore=%v\nafter=%v", sortedSnapshot(before), sortedSnapshot(after))
			}
			assertVaultDecryptsWithKey(t, manager, oldKey)
			assertNoRekeyArtifacts(t, manager)
			if _, err := os.Stat(filepath.Join(manager.configPath, rekeyManifestFile)); !os.IsNotExist(err) {
				t.Fatalf("manifest remains: %v", err)
			}
		})
	}
}

func TestRekeyCommitFailures(t *testing.T) {
	var cases []struct {
		operation string
		index     int
	}
	for i := 0; i < 4; i++ {
		cases = append(cases,
			struct {
				operation string
				index     int
			}{"rename_old", i},
			struct {
				operation string
				index     int
			}{"rename_new", i},
		)
	}
	cases = append(cases,
		struct {
			operation string
			index     int
		}{"manifest_SWITCH_METADATA", 0},
		struct {
			operation string
			index     int
		}{"metadata_write", 0},
		struct {
			operation string
			index     int
		}{"metadata_file_fsync", 0},
		struct {
			operation string
			index     int
		}{"metadata_dir_fsync", 0},
		struct {
			operation string
			index     int
		}{"manifest_COMMITTED", 0},
		struct {
			operation string
			index     int
		}{"manifest_CLEANUP", 0},
		struct {
			operation string
			index     int
		}{"cleanup_remove", 0},
	)
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%d", tc.operation, tc.index), func(t *testing.T) {
			manager, oldKey := setupRekeyVault(t)
			inputs := makeRekeyTestInputs(t, "new-password")
			manager.rekeyHooks = &rekeyHooks{before: func(operation string, index int) error {
				if operation == tc.operation && index == tc.index {
					return errInjectedRekey
				}
				return nil
			}}
			if _, err := manager.Rekey(oldKey, inputs.key, inputs.salt, inputs.passwordKey, crypto.DefaultIterations); err == nil {
				t.Fatal("Rekey succeeded at injected commit failure")
			}
			manager.rekeyHooks = nil
			if err := manager.RecoverRekey(); err != nil {
				t.Fatalf("recovery after commit failure: %v", err)
			}
			oldOK, err := manager.VerifyKey(oldKey)
			if err != nil {
				t.Fatal(err)
			}
			newOK, err := manager.VerifyKey(inputs.key)
			if err != nil {
				t.Fatal(err)
			}
			if oldOK == newOK {
				t.Fatalf("generation invariant violated: oldOK=%v newOK=%v", oldOK, newOK)
			}
			if oldOK {
				assertVaultDecryptsWithKey(t, manager, oldKey)
			} else {
				assertVaultDecryptsWithKey(t, manager, inputs.key)
			}
			assertNoRekeyArtifacts(t, manager)
		})
	}
}
