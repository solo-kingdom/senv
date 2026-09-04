//go:build unix

package session

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func fallbackLifecycleLockName() string {
	return fmt.Sprintf(".senv-session-%d.lock", os.Getuid())
}

// withFallbackLifecycleLock serializes fallback cache creation, enumeration,
// replacement, and cleanup. The lock itself is opened no-follow under a
// runtime root that was already proven memory-backed and non-symlinked.
func withFallbackLifecycleLock(tempRoot string, fn func() error) error {
	if err := validateRuntimeRoot(tempRoot); err != nil {
		return err
	}
	path := filepath.Join(tempRoot, fallbackLifecycleLockName())
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("open fallback session lock: %w", err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat fallback session lock: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("fallback session lock is not a private regular file")
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return fmt.Errorf("secure fallback session lock: %w", err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return fmt.Errorf("acquire fallback session lock: %w", err)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return fn()
}
