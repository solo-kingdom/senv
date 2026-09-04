//go:build unix

package storage

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestVaultMutationLockSerializesSameVault(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	first, err := acquireVaultMutationLock(config, time.Second)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	acquired := make(chan *vaultMutationLock, 1)
	errs := make(chan error, 1)
	go func() {
		second, err := acquireVaultMutationLock(config, time.Second)
		if err != nil {
			errs <- err
			return
		}
		acquired <- second
	}()
	select {
	case second := <-acquired:
		_ = second.release()
		t.Fatal("same-vault lock acquired before release")
	case err := <-errs:
		t.Fatalf("second lock failed early: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err := first.release(); err != nil {
		t.Fatalf("release first: %v", err)
	}
	select {
	case second := <-acquired:
		_ = second.release()
	case err := <-errs:
		t.Fatalf("second lock: %v", err)
	case <-time.After(time.Second):
		t.Fatal("second lock did not acquire after release")
	}
}

func TestVaultMutationLockDifferentVaultsIndependent(t *testing.T) {
	base := t.TempDir()
	first, err := acquireVaultMutationLock(filepath.Join(base, "one"), time.Second)
	if err != nil {
		t.Fatalf("first vault: %v", err)
	}
	defer first.release()
	second, err := acquireVaultMutationLock(filepath.Join(base, "two"), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("independent vault blocked: %v", err)
	}
	_ = second.release()
}

func TestVaultMutationLockTimeout(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	first, err := acquireVaultMutationLock(config, time.Second)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer first.release()
	start := time.Now()
	_, err = acquireVaultMutationLock(config, 40*time.Millisecond)
	if !errors.Is(err, ErrVaultMutationLockTimeout) {
		t.Fatalf("error = %v, want ErrVaultMutationLockTimeout", err)
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatalf("timeout returned too early: %s", time.Since(start))
	}
}

func TestVaultMutationLockReleasedWhenProcessExits(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	cmd := exec.Command(os.Args[0], "-test.run=^TestVaultMutationLockProcessHelper$")
	cmd.Env = append(os.Environ(), "SENV_LOCK_HELPER=1", "SENV_LOCK_CONFIG="+config)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v: %s", err, out)
	}
	lock, err := acquireVaultMutationLock(config, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("lock remained held after process exit: %v", err)
	}
	_ = lock.release()
}

func TestVaultMutationLockProcessHelper(t *testing.T) {
	if os.Getenv("SENV_LOCK_HELPER") != "1" {
		return
	}
	lock, err := acquireVaultMutationLock(os.Getenv("SENV_LOCK_CONFIG"), time.Second)
	if err != nil {
		os.Exit(2)
	}
	_ = lock // Intentionally rely on process-exit descriptor cleanup.
	os.Exit(0)
}
