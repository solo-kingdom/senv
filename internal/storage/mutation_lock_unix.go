//go:build unix

package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// vaultLockAcquires 统计 vault mutation 锁的成功获取次数。生产路径仅原子
// 自增；测试在断言前复位，用于验证「单趟批量读取只获取一次锁」
// （tui-startup-perf D2/D3 的排它锁次数不随条目数增长）。
var vaultLockAcquires atomic.Int64

type vaultMutationLock struct {
	fd int
}

func acquireVaultMutationLock(configPath string, timeout time.Duration) (*vaultMutationLock, error) {
	if err := EnsurePrivateDir(configPath, 0o700); err != nil {
		return nil, fmt.Errorf("prepare vault lock directory: %w", err)
	}
	path := filepath.Join(configPath, vaultMutationLockFile)
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open vault mutation lock: %w", err)
	}
	closeOnError := func(err error) (*vaultMutationLock, error) {
		_ = unix.Close(fd)
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return closeOnError(fmt.Errorf("stat vault mutation lock: %w", err))
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return closeOnError(fmt.Errorf("vault mutation lock is not a regular file"))
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return closeOnError(fmt.Errorf("secure vault mutation lock: %w", err))
	}

	deadline := time.Now().Add(timeout)
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			vaultLockAcquires.Add(1)
			return &vaultMutationLock{fd: fd}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return closeOnError(fmt.Errorf("acquire vault mutation lock: %w", err))
		}
		if timeout <= 0 || !time.Now().Before(deadline) {
			return closeOnError(fmt.Errorf("%w after %s", ErrVaultMutationLockTimeout, timeout))
		}
		time.Sleep(minDuration(10*time.Millisecond, time.Until(deadline)))
	}
}

func (l *vaultMutationLock) release() error {
	if l == nil || l.fd < 0 {
		return nil
	}
	fd := l.fd
	l.fd = -1
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	closeErr := unix.Close(fd)
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
