// Agent 配置写事务：每个 touched path 保存独立快照与同目录备份。事务期间
// 使用进程内路径锁串行化同机 CLI/TUI/MCP 的同路径写入；跨进程锁是 Non-goal。
package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wii/senv/internal/storage"
)

// pathLockRegistry stores one mutex per canonical absolute path. Entries are
// small and intentionally retained for process lifetime to avoid reset races.
var pathLockRegistry = struct {
	sync.Mutex
	locks map[string]*sync.Mutex
}{locks: map[string]*sync.Mutex{}}

func lockPaths(paths ...string) func() {
	normalized := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return func() {}
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		normalized = append(normalized, abs)
	}
	// Sort before acquisition so nested/overlapping path lists cannot deadlock.
	sortStrings(normalized)
	locks := make([]*sync.Mutex, 0, len(normalized))
	for _, path := range normalized {
		pathLockRegistry.Lock()
		mu, ok := pathLockRegistry.locks[path]
		if !ok {
			mu = &sync.Mutex{}
			pathLockRegistry.locks[path] = mu
		}
		pathLockRegistry.Unlock()
		mu.Lock()
		locks = append(locks, mu)
	}
	return func() {
		for _, mu := range locks {
			mu.Unlock()
		}
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

type pathSnapshot struct {
	path     string
	exists   bool
	data     []byte
	backup   string
	touched  bool
	restored bool
}

type configTransaction struct {
	snapshots  []*pathSnapshot
	unlock     func()
	committed  bool
	renameHook func(oldName, newName string) error // package-private test seam
}

func newConfigTransaction(paths ...string) (*configTransaction, error) {
	unique := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve config path %q: %w", path, err)
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		unique = append(unique, abs)
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("config transaction has no target")
	}
	unlock := lockPaths(unique...)
	tx := &configTransaction{unlock: unlock}
	rollback := func() { _ = tx.rollback() }
	for _, path := range unique {
		if err := storageEnsurePrivateDir(filepath.Dir(path)); err != nil {
			rollback()
			return nil, err
		}
		snapshot := &pathSnapshot{path: path}
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			snapshot.exists = true
			snapshot.data = data
		case os.IsNotExist(err):
			snapshot.exists = false
		default:
			rollback()
			return nil, fmt.Errorf("snapshot config %s: %w", path, err)
		}
		if snapshot.exists {
			backup, err := createPrivateBackup(path, data)
			if err != nil {
				rollback()
				return nil, err
			}
			snapshot.backup = backup
		}
		tx.snapshots = append(tx.snapshots, snapshot)
	}
	return tx, nil
}

func createPrivateBackup(path string, data []byte) (string, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".senv-bak-*")
	if err != nil {
		return "", fmt.Errorf("create config backup: %w", err)
	}
	name := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(name)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return "", fmt.Errorf("chmod config backup: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return "", fmt.Errorf("write config backup: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("fsync config backup: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("close config backup: %w", err)
	}
	return name, nil
}

func (tx *configTransaction) contains(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, snapshot := range tx.snapshots {
		if snapshot.path == abs {
			return true
		}
	}
	return false
}

// write replaces path via a same-directory temporary file. Rename happens only
// after data and file descriptor are durable; the directory is synced after.
func (tx *configTransaction) write(path string, data []byte) error {
	if tx == nil {
		return fmt.Errorf("config transaction is nil")
	}
	if tx.committed {
		return fmt.Errorf("config transaction is committed")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve config path %q: %w", path, err)
	}
	abs = filepath.Clean(abs)
	snapshot := tx.find(abs)
	if snapshot == nil {
		return fmt.Errorf("config path %s is not covered by transaction", path)
	}
	if err := storageEnsurePrivateDir(filepath.Dir(abs)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".senv-*.tmp")
	if err != nil {
		return fmt.Errorf("create config temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod config temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write config temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("fsync config temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close config temp file: %w", err)
	}
	if tx.renameHook != nil {
		if err := tx.renameHook(tmpName, abs); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
	}
	if err := os.Rename(tmpName, abs); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace config: %w", err)
	}
	snapshot.touched = true
	if err := syncDir(filepath.Dir(abs)); err != nil {
		return fmt.Errorf("fsync config directory: %w", err)
	}
	return nil
}

func (tx *configTransaction) find(path string) *pathSnapshot {
	for _, snapshot := range tx.snapshots {
		if snapshot.path == path {
			return snapshot
		}
	}
	return nil
}

// rollback restores touched paths in reverse order. Each successful restore
// removes only that path's backup; a failed restore leaves its good backup.
func (tx *configTransaction) rollback() error {
	if tx == nil || tx.committed {
		return nil
	}
	var firstErr error
	for i := len(tx.snapshots) - 1; i >= 0; i-- {
		snapshot := tx.snapshots[i]
		if !snapshot.touched {
			continue
		}
		var err error
		if snapshot.exists {
			err = privateWrite(snapshot.path, snapshot.data)
		} else {
			err = os.Remove(snapshot.path)
			if os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("restore config %s: %w", snapshot.path, err)
			}
			continue
		}
		if err := syncDir(filepath.Dir(snapshot.path)); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("fsync restored config directory %s: %w", filepath.Dir(snapshot.path), err)
			continue
		}
		if snapshot.backup != "" {
			if err := os.Remove(snapshot.backup); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("remove restored config backup: %w", err)
			} else if err == nil {
				snapshot.backup = ""
			}
		}
		snapshot.restored = true
	}
	return firstErr
}

// commit removes durable per-path backups after the pointer has been updated.
func (tx *configTransaction) commit() error {
	if tx == nil {
		return fmt.Errorf("config transaction is nil")
	}
	var firstErr error
	for _, snapshot := range tx.snapshots {
		if snapshot.backup == "" {
			continue
		}
		if err := os.Remove(snapshot.backup); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("remove config backup: %w", err)
		} else if err == nil {
			snapshot.backup = ""
		}
	}
	tx.committed = true
	tx.unlock()
	return firstErr
}

func privateWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".senv-restore-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return err
	}
	return handle.Close()
}

// storageEnsurePrivateDir wraps the storage helper so transaction code stays
// independent of the storage manager while reusing its symlink-safe tightening.
func storageEnsurePrivateDir(dir string) error {
	return storage.EnsurePrivateDir(dir, 0o700)
}
