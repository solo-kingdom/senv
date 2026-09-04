package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSensitiveFile_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := WriteSensitiveFile(path, []byte("{}"), 0o700, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("new file perm = %o, want 600", got)
	}
}

func TestWriteSensitiveFile_TightensExistingLooseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Simulate a vault created by an older version: world-readable file.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteSensitiveFile(path, []byte("new"), 0o700, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("rewritten file perm = %o, want 600 (old os.WriteFile behavior kept 644)", got)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Errorf("content = %q err = %v, want 'new'", data, err)
	}
}

func TestWriteSensitiveFile_TightensLooseParentDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "config")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	if err := WriteSensitiveFile(filepath.Join(sub, "settings.json"), []byte("{}"), 0o700, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := os.Stat(sub)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o700 {
		t.Errorf("parent dir perm = %o, want 700", got)
	}
}

func TestWriteSensitiveFile_MkdirDeep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "settings.json")

	if err := WriteSensitiveFile(path, []byte("{}"), 0o700, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, d := range []string{
		filepath.Join(dir, "a"),
		filepath.Join(dir, "a", "b"),
		filepath.Join(dir, "a", "b", "c"),
	} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %s: %v", d, err)
		}
		if got := fi.Mode().Perm(); got != 0o700 {
			t.Errorf("dir %s perm = %o, want 700", d, got)
		}
	}
}

func TestEnsurePrivateDirRejectsIntermediateSymlink(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "linked")); err != nil {
		t.Fatal(err)
	}

	err := EnsurePrivateDir(filepath.Join(base, "linked", "vault"), 0o700)
	if err == nil {
		t.Fatal("EnsurePrivateDir followed an intermediate symlink")
	}
	if _, err := os.Lstat(filepath.Join(outside, "vault")); !os.IsNotExist(err) {
		t.Fatalf("outside directory changed through symlink: %v", err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("outside mode changed to %04o", info.Mode().Perm())
	}
}
