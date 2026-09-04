package storage

import (
	"errors"
	"fmt"
	"time"
)

const vaultMutationLockFile = ".senv-vault.lock"

var (
	// ErrVaultMutationLockTimeout reports that another process retained the
	// vault mutation lock past the bounded wait. Callers may use errors.Is.
	ErrVaultMutationLockTimeout = errors.New("vault mutation lock timeout")
	// ErrRekeyRecoveryRequired marks a journal that cannot be recovered safely.
	ErrRekeyRecoveryRequired = errors.New("unfinished rekey requires recovery")
)

const defaultVaultMutationLockTimeout = 30 * time.Second

// WithVaultMutation serializes a complete logical mutation with rekey and
// performs recovery before exposing the lock-held manager to fn. The callback
// must use the supplied manager, not the receiver, for nested storage calls.
func (m *Manager) WithVaultMutation(fn func(*Manager) error) error {
	if m == nil {
		return fmt.Errorf("storage manager is nil")
	}
	if m.mutationLocked {
		return fn(m)
	}
	lock, err := acquireVaultMutationLock(m.configPath, defaultVaultMutationLockTimeout)
	if err != nil {
		return err
	}
	defer lock.release()

	locked := *m
	locked.mutationLocked = true
	if err := locked.recoverRekeyLocked(); err != nil {
		return err
	}
	return fn(&locked)
}

func (m *Manager) mutate(fn func(*Manager) error) error {
	if m.mutationLocked {
		return fn(m)
	}
	return m.WithVaultMutation(fn)
}

// withVaultRead holds the vault mutation lock from recovery through the full
// observation, so a reader cannot see the mixed on-disk generation used while
// rekey is switching files.
func withVaultRead[T any](m *Manager, fn func(*Manager) (T, error)) (T, error) {
	if m.mutationLocked {
		return fn(m)
	}
	var result T
	err := m.WithVaultMutation(func(locked *Manager) error {
		var innerErr error
		result, innerErr = fn(locked)
		return innerErr
	})
	return result, err
}

// RecoverRekey runs the same recovery gate used by every mutation. It is safe
// to call at read/authentication boundaries before opening encrypted data.
func (m *Manager) RecoverRekey() error {
	return m.WithVaultMutation(func(*Manager) error { return nil })
}

func (m *Manager) requireCurrentKey(key []byte) error {
	ok, err := m.VerifyKey(key)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: mutation key no longer matches current metadata", ErrDataDesync)
	}
	return nil
}
