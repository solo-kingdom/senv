//go:build unix

package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncLockRejectsSymlinkDataRoot(t *testing.T) {
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(t.TempDir(), "linked-data")
	if err := os.Symlink(outside, dataPath); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireSyncLock(dataPath, false); err == nil {
		_ = lock.release()
		t.Fatal("sync lock accepted a symlinked data root")
	}
	if _, err := os.Lstat(filepath.Join(outside, syncLockFileName)); !os.IsNotExist(err) {
		t.Fatalf("sync lock created outside the data root: %v", err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("outside directory mode changed to %04o", info.Mode().Perm())
	}
}
