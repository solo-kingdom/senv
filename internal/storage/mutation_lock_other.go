//go:build !unix

package storage

import (
	"fmt"
	"time"
)

type vaultMutationLock struct{}

func acquireVaultMutationLock(_ string, _ time.Duration) (*vaultMutationLock, error) {
	return nil, fmt.Errorf("vault mutation lock is unsupported on this platform")
}

func (l *vaultMutationLock) release() error { return nil }
