//go:build unix

package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		return nil, err
	}
	// OpenFile 的 mode 只在创建时生效；旧目录/旧锁文件也需要收敛到安全权限。
	if err := os.Chmod(dataPath, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataPath, syncLockFileName)
	before, statErr := os.Lstat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if statErr == nil && !before.Mode().IsRegular() {
		return nil, fmt.Errorf("sync lock %s is not a regular file", path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !after.Mode().IsRegular() || (statErr == nil && !os.SameFile(before, after)) {
		f.Close()
		return nil, fmt.Errorf("sync lock %s changed while opening", path)
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
