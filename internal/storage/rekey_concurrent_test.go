package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
)

func TestStorageRekeyConcurrentMutation(t *testing.T) {
	manager, oldKey := setupRekeyVault(t)
	inputs := makeRekeyTestInputs(t, "new-password")
	paused := make(chan struct{})
	resume := make(chan struct{})
	manager.rekeyHooks = &rekeyHooks{checkpoint: func(point rekeyCheckpoint) {
		if point == rekeyCheckpointSwitchData {
			close(paused)
			<-resume
		}
	}}
	rekeyDone := make(chan error, 1)
	go func() {
		_, err := manager.Rekey(oldKey, inputs.key, inputs.salt, inputs.passwordKey, crypto.DefaultIterations)
		rekeyDone <- err
	}()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("rekey did not reach paused durable stage")
	}

	mutationDone := make(chan error, 1)
	go func() {
		mutationDone <- manager.SaveEnvVarWithKey("default", "LATE", &EnvVarEntry{Value: "must-not-appear"}, oldKey)
	}()
	select {
	case err := <-mutationDone:
		t.Fatalf("mutation interleaved with rekey: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	close(resume)
	if err := <-rekeyDone; err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if err := <-mutationDone; !errors.Is(err, ErrDataDesync) {
		t.Fatalf("stale mutation error = %v, want ErrDataDesync", err)
	}
	manager.rekeyHooks = nil
	if _, err := os.Stat(filepath.Join(manager.dataPath, EnvDirName, "default", "LATE.enc")); !os.IsNotExist(err) {
		t.Fatalf("unjournaled entry was created: %v", err)
	}
	assertVaultDecryptsWithKey(t, manager, inputs.key)
}

func TestStorageRekeyConcurrentReadIsSerialized(t *testing.T) {
	manager, oldKey := setupRekeyVault(t)
	inputs := makeRekeyTestInputs(t, "new-password")
	paused := make(chan struct{})
	resume := make(chan struct{})
	manager.rekeyHooks = &rekeyHooks{checkpoint: func(point rekeyCheckpoint) {
		if point == rekeyCheckpointSwitchData {
			close(paused)
			<-resume
		}
	}}
	rekeyDone := make(chan error, 1)
	go func() {
		_, err := manager.Rekey(oldKey, inputs.key, inputs.salt, inputs.passwordKey, crypto.DefaultIterations)
		rekeyDone <- err
	}()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("rekey did not reach paused durable stage")
	}

	readDone := make(chan error, 1)
	go func() {
		_, err := manager.ListEnvGroups()
		readDone <- err
	}()
	select {
	case err := <-readDone:
		t.Fatalf("read interleaved with rekey: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	close(resume)
	if err := <-rekeyDone; err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if err := <-readDone; err != nil {
		t.Fatalf("serialized read failed: %v", err)
	}
	manager.rekeyHooks = nil
}
