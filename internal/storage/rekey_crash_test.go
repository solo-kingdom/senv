package storage

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

const rekeyCrashExitCode = 86

func TestRekeyCrashHarness(t *testing.T) {
	switch os.Getenv("SENV_TEST_REKEY_SUBPROCESS") {
	case "crash":
		runRekeyCrashChild(t)
		return
	case "recover":
		runRekeyRecoveryChild(t)
		return
	}
	manager, _ := setupRekeyVault(t)
	runRekeyCrashProcess(t, manager, rekeyCheckpointSwitchData, 1)
	if _, err := os.Stat(filepath.Join(manager.configPath, rekeyManifestFile)); err != nil {
		t.Fatalf("crash harness did not retain journal: %v", err)
	}
	runRekeyRecoveryProcess(t, manager)
	assertNoRekeyArtifacts(t, manager)
}

func TestRekeyCrashRecovery(t *testing.T) {
	cases := []struct {
		name       string
		point      rekeyCheckpoint
		occurrence int
	}{
		{name: "PREPARE", point: rekeyCheckpointPrepare, occurrence: 1},
		{name: "SWITCH_DATA prepared", point: rekeyCheckpointSwitchData, occurrence: 1},
		{name: "SWITCH_DATA partial", point: rekeyCheckpointDataEntry, occurrence: 1},
		{name: "SWITCH_METADATA", point: rekeyCheckpointSwitchMetadata, occurrence: 1},
		{name: "metadata durable before COMMITTED", point: rekeyCheckpointMetadata, occurrence: 1},
		{name: "COMMITTED", point: rekeyCheckpointCommitted, occurrence: 1},
		{name: "CLEANUP", point: rekeyCheckpointCleanup, occurrence: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, _ := setupRekeyVault(t)
			runRekeyCrashProcess(t, manager, tc.point, tc.occurrence)
			runRekeyRecoveryProcess(t, manager)
			assertNoRekeyArtifacts(t, manager)
		})
	}
}

func runRekeyCrashProcess(t *testing.T, manager *Manager, point rekeyCheckpoint, occurrence int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRekeyCrashHarness$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"SENV_TEST_REKEY_SUBPROCESS=crash",
		"SENV_TEST_REKEY_CONFIG="+manager.configPath,
		"SENV_TEST_REKEY_DATA="+manager.dataPath,
		"SENV_TEST_REKEY_POINT="+string(point),
		"SENV_TEST_REKEY_OCCURRENCE="+strconv.Itoa(occurrence),
	)
	out, err := cmd.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != rekeyCrashExitCode {
		t.Fatalf("crash child exit=%v, want %d; output=%s", err, rekeyCrashExitCode, out)
	}
}

func runRekeyRecoveryProcess(t *testing.T, manager *Manager) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRekeyCrashHarness$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"SENV_TEST_REKEY_SUBPROCESS=recover",
		"SENV_TEST_REKEY_CONFIG="+manager.configPath,
		"SENV_TEST_REKEY_DATA="+manager.dataPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("recovery child: %v; output=%s", err, out)
	}
}

func runRekeyCrashChild(t *testing.T) {
	manager := NewManager(os.Getenv("SENV_TEST_REKEY_CONFIG"), os.Getenv("SENV_TEST_REKEY_DATA"))
	metadata, err := manager.LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		t.Fatal(err)
	}
	iterations, err := metadata.ValidatedKDFIterations()
	if err != nil {
		t.Fatal(err)
	}
	oldKey := crypto.DeriveKeyWithIterations("test-password", salt, iterations)
	inputs := makeRekeyTestInputs(t, "new-password")
	point := rekeyCheckpoint(os.Getenv("SENV_TEST_REKEY_POINT"))
	occurrence, err := strconv.Atoi(os.Getenv("SENV_TEST_REKEY_OCCURRENCE"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	manager.rekeyHooks = &rekeyHooks{checkpoint: func(got rekeyCheckpoint) {
		if got == point {
			seen++
			if seen == occurrence {
				os.Exit(rekeyCrashExitCode)
			}
		}
	}}
	if _, err := manager.Rekey(oldKey, inputs.key, inputs.salt, inputs.passwordKey, crypto.DefaultIterations); err != nil {
		t.Fatalf("Rekey before crash point: %v", err)
	}
	t.Fatalf("rekey completed without reaching crash point %s/%d", point, occurrence)
}

func runRekeyRecoveryChild(t *testing.T) {
	manager := NewManager(os.Getenv("SENV_TEST_REKEY_CONFIG"), os.Getenv("SENV_TEST_REKEY_DATA"))
	if err := manager.RecoverRekey(); err != nil {
		t.Fatalf("RecoverRekey: %v", err)
	}
	metadata, err := manager.LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		t.Fatal(err)
	}
	iterations, err := metadata.ValidatedKDFIterations()
	if err != nil {
		t.Fatal(err)
	}
	oldKey := crypto.DeriveKeyWithIterations("test-password", salt, iterations)
	newKey := crypto.DeriveKeyWithIterations("new-password", salt, iterations)
	oldOK, err := manager.VerifyKey(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	newOK, err := manager.VerifyKey(newKey)
	if err != nil {
		t.Fatal(err)
	}
	if oldOK == newOK {
		t.Fatalf("mixed or unavailable generation after process recovery: old=%v new=%v", oldOK, newOK)
	}
	if oldOK {
		assertVaultDecryptsWithKey(t, manager, oldKey)
		fmt.Fprintln(os.Stderr, "recovered generation: old")
	} else {
		assertVaultDecryptsWithKey(t, manager, newKey)
		fmt.Fprintln(os.Stderr, "recovered generation: new")
	}
	assertNoRekeyArtifacts(t, manager)
}
