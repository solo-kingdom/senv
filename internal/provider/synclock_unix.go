//go:build unix

package provider

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/wii/senv/internal/storage"
	"golang.org/x/sys/unix"
)

// errSyncLocked 表示同步锁正被其他进程持有（非阻塞获取时）
var errSyncLocked = errors.New("sync lock held by another process")

// syncLockDir 数据目录内的同步锁文件（不含敏感内容，仅用于进程间互斥）
const syncLockFileName = ".senv-sync.lock"

// syncLock 是 dataPath 上的进程间排它锁；持有期间同步段（pull/push/状态读写）串行化。
// flock 随进程退出自动释放，崩溃安全。
type syncLock struct {
	f *os.File
}

// acquireSyncLock 获取同步锁：blocking=true 时等待（手动同步），false 时拿不到立即返回 errSyncLocked（自动同步跳过）。
func acquireSyncLock(dataPath string, blocking bool) (*syncLock, error) {
	if err := storage.EnsurePrivateDir(dataPath, 0o700); err != nil {
		return nil, err
	}
	dirFD, err := unix.Open(dataPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open sync lock directory: %w", err)
	}
	defer unix.Close(dirFD)

	fd, err := unix.Openat(dirFD, syncLockFileName, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open sync lock: %w", err)
	}
	f := os.NewFile(uintptr(fd), syncLockFileName)
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open sync lock: invalid file descriptor")
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("sync lock is not a regular file")
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}

	how := syscall.LOCK_EX
	if !blocking {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errSyncLocked
		}
		return nil, err
	}
	return &syncLock{f: f}, nil
}

// release 释放锁并关闭文件描述符
func (l *syncLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	closeErr := l.f.Close()
	l.f = nil
	if err != nil {
		return err
	}
	return closeErr
}
