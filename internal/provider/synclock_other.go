//go:build !unix

package provider

import "errors"

// 非 POSIX 平台（windows）回退：无法 flock，退化为无锁。
// 已知限制：同机并发进程间不互斥，多进程并发写存在同步状态竞争风险（见 design Risks）。
var errSyncLocked = errors.New("sync lock held by another process")

const syncLockFileName = ".senv-sync.lock"

type syncLock struct{}

func acquireSyncLock(_ string, _ bool) (*syncLock, error) { return &syncLock{}, nil }

func (l *syncLock) release() error { return nil }
