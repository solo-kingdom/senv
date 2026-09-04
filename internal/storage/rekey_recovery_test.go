package storage

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func interruptRekeyAt(t *testing.T, manager *Manager, oldKey []byte, inputs rekeyTestInputs, point rekeyCheckpoint, occurrence int) {
	t.Helper()
	seen := 0
	manager.rekeyHooks = &rekeyHooks{checkpoint: func(got rekeyCheckpoint) {
		if got != point {
			return
		}
		seen++
		if seen == occurrence {
			panic("test rekey interruption")
		}
	}}
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_, _ = manager.Rekey(oldKey, inputs.key, inputs.salt, inputs.passwordKey, crypto.DefaultIterations)
	}()
	manager.rekeyHooks = nil
	if !panicked {
		t.Fatalf("rekey did not interrupt at %s occurrence %d", point, occurrence)
	}
	if _, err := os.Stat(filepath.Join(manager.configPath, rekeyManifestFile)); err != nil {
		t.Fatalf("interruption left no manifest: %v", err)
	}
}

func TestRekeyRecovery(t *testing.T) {
	cases := []struct {
		name       string
		point      rekeyCheckpoint
		occurrence int
		wantNew    bool
	}{
		{name: "PREPARE manifest only", point: rekeyCheckpointPrepare, occurrence: 1},
		{name: "PREPARED generations", point: rekeyCheckpointSwitchData, occurrence: 1},
		{name: "data switched halfway", point: rekeyCheckpointDataEntry, occurrence: 1},
		{name: "DATA_SWITCHED", point: rekeyCheckpointSwitchMetadata, occurrence: 1},
		{name: "metadata switched before marker", point: rekeyCheckpointMetadata, occurrence: 1, wantNew: true},
		{name: "COMMITTED", point: rekeyCheckpointCommitted, occurrence: 1, wantNew: true},
		{name: "cleanup in progress", point: rekeyCheckpointCleanup, occurrence: 1, wantNew: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, oldKey := setupRekeyVault(t)
			inputs := makeRekeyTestInputs(t, "new-password")
			interruptRekeyAt(t, manager, oldKey, inputs, tc.point, tc.occurrence)
			if err := manager.RecoverRekey(); err != nil {
				t.Fatalf("RecoverRekey: %v", err)
			}
			oldOK, err := manager.VerifyKey(oldKey)
			if err != nil {
				t.Fatal(err)
			}
			newOK, err := manager.VerifyKey(inputs.key)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNew {
				if oldOK || !newOK {
					t.Fatalf("wanted new generation: oldOK=%v newOK=%v", oldOK, newOK)
				}
				assertVaultDecryptsWithKey(t, manager, inputs.key)
			} else {
				if !oldOK || newOK {
					t.Fatalf("wanted old generation: oldOK=%v newOK=%v", oldOK, newOK)
				}
				assertVaultDecryptsWithKey(t, manager, oldKey)
			}
			assertNoRekeyArtifacts(t, manager)
		})
	}

	t.Run("corrupt manifest fails closed and preserves materials", func(t *testing.T) {
		manager, oldKey := setupRekeyVault(t)
		inputs := makeRekeyTestInputs(t, "new-password")
		interruptRekeyAt(t, manager, oldKey, inputs, rekeyCheckpointSwitchData, 1)
		manifestPath := filepath.Join(manager.configPath, rekeyManifestFile)
		if err := os.WriteFile(manifestPath, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := snapshotVault(t, manager)
		err := manager.RecoverRekey()
		if !errors.Is(err, ErrRekeyRecoveryRequired) {
			t.Fatalf("error = %v, want ErrRekeyRecoveryRequired", err)
		}
		after := snapshotVault(t, manager)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("failed recovery mutated materials\nbefore=%v\nafter=%v", sortedSnapshot(before), sortedSnapshot(after))
		}
	})
}
