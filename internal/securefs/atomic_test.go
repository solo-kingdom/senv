//go:build linux || darwin

package securefs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const deterministicTempHex = "00112233445566778899aabbccddeeff"

func TestAtomicWriteNewFileIsPrivateAndComplete(t *testing.T) {
	rootDir := t.TempDir()
	root := openTestRoot(t, rootDir)
	payload := []byte(strings.Repeat("complete-payload-", 64))
	root.atomicHooks = &atomicWriteHooks{
		randomHex: fixedTempName,
		write: func(fd int, data []byte) (int, error) {
			if len(data) > 7 {
				data = data[:7]
			}
			return unix.Write(fd, data)
		},
	}

	if err := root.AtomicWrite([]string{"secret.enc"}, payload, 0o600); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	assertFileBytes(t, filepath.Join(rootDir, "secret.enc"), payload)
	assertFileMode(t, filepath.Join(rootDir, "secret.enc"), 0o600)
	assertNoAtomicTemps(t, rootDir)
}

func TestAtomicWriteTightensModeAndPreservesStricterMode(t *testing.T) {
	tests := []struct {
		name      string
		existing  fs.FileMode
		requested fs.FileMode
		want      fs.FileMode
	}{
		{name: "tighten loose file", existing: 0o644, requested: 0o600, want: 0o600},
		{name: "preserve stricter file", existing: 0o400, requested: 0o600, want: 0o400},
		{name: "explicit wider request cannot widen", existing: 0o600, requested: 0o644, want: 0o600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			path := filepath.Join(rootDir, "target")
			if err := os.WriteFile(path, []byte("old"), tt.existing); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tt.existing); err != nil {
				t.Fatal(err)
			}
			root := openTestRoot(t, rootDir)
			root.atomicHooks = &atomicWriteHooks{randomHex: fixedTempName}

			if err := root.AtomicWrite([]string{"target"}, []byte("new"), tt.requested); err != nil {
				t.Fatalf("AtomicWrite: %v", err)
			}
			assertFileContent(t, path, "new")
			assertFileMode(t, path, tt.want)
			assertNoAtomicTemps(t, rootDir)
		})
	}
}

func TestAtomicWriteFailuresKeepCompleteVersion(t *testing.T) {
	injected := errors.New("injected atomic-write failure")
	oldPayload := []byte(strings.Repeat("old-version-", 64))
	newPayload := []byte(strings.Repeat("new-version-", 64))

	tests := []struct {
		name      string
		configure func(*atomicWriteHooks)
		wantData  []byte
	}{
		{
			name: "write",
			configure: func(h *atomicWriteHooks) {
				h.write = func(_ int, _ []byte) (int, error) { return 0, injected }
			},
			wantData: oldPayload,
		},
		{
			name: "file fsync",
			configure: func(h *atomicWriteHooks) {
				h.fileSync = func(_ int) error { return injected }
			},
			wantData: oldPayload,
		},
		{
			name: "rename",
			configure: func(h *atomicWriteHooks) {
				h.rename = func(_ int, _ string, _ int, _ string) error { return injected }
			},
			wantData: oldPayload,
		},
		{
			name: "directory fsync",
			configure: func(h *atomicWriteHooks) {
				h.dirSync = func(_ int) error { return injected }
			},
			wantData: newPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			target := filepath.Join(rootDir, "target")
			if err := os.WriteFile(target, oldPayload, 0o600); err != nil {
				t.Fatal(err)
			}
			root := openTestRoot(t, rootDir)
			hooks := &atomicWriteHooks{randomHex: fixedTempName}
			tt.configure(hooks)
			root.atomicHooks = hooks

			err := root.AtomicWrite([]string{"target"}, newPayload, 0o600)
			if !errors.Is(err, injected) {
				t.Fatalf("AtomicWrite error = %v, want injected error", err)
			}
			assertFileBytes(t, target, tt.wantData)
			assertFileMode(t, target, 0o600)
			assertNoAtomicTemps(t, rootDir)
		})
	}
}

func TestAtomicWriteFailureDoesNotCreateTarget(t *testing.T) {
	rootDir := t.TempDir()
	root := openTestRoot(t, rootDir)
	injected := errors.New("injected write failure")
	root.atomicHooks = &atomicWriteHooks{
		randomHex: fixedTempName,
		write:     func(_ int, _ []byte) (int, error) { return 0, injected },
	}

	err := root.AtomicWrite([]string{"new-target"}, []byte("secret"), 0o600)
	if !errors.Is(err, injected) {
		t.Fatalf("AtomicWrite error = %v, want injected error", err)
	}
	if _, err := os.Lstat(filepath.Join(rootDir, "new-target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new target exists after failed write: %v", err)
	}
	assertNoAtomicTemps(t, rootDir)
}

func TestAtomicWriteRejectsTargetAndParentSymlinks(t *testing.T) {
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("target", func(t *testing.T) {
		rootDir := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(rootDir, "target")); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.AtomicWrite([]string{"target"}, []byte("new"), 0o600); !errors.Is(err, ErrSymlink) {
			t.Fatalf("AtomicWrite(target symlink) error = %v, want ErrSymlink", err)
		}
		assertNoAtomicTemps(t, rootDir)
	})

	t.Run("parent", func(t *testing.T) {
		rootDir := t.TempDir()
		if err := os.Symlink(outsideDir, filepath.Join(rootDir, "parent")); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.AtomicWrite([]string{"parent", "sentinel"}, []byte("new"), 0o600); !errors.Is(err, ErrSymlink) {
			t.Fatalf("AtomicWrite(parent symlink) error = %v, want ErrSymlink", err)
		}
		assertNoAtomicTemps(t, rootDir)
	})
	assertFileContent(t, outside, "outside")
}

func TestAtomicWriteRejectsNonPermissionMode(t *testing.T) {
	rootDir := t.TempDir()
	root := openTestRoot(t, rootDir)
	err := root.AtomicWrite([]string{"target"}, []byte("new"), fs.ModeSetuid|0o600)
	if err == nil {
		t.Fatal("AtomicWrite accepted setuid mode")
	}
	if _, statErr := os.Lstat(filepath.Join(rootDir, "target")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target exists after invalid mode: %v", statErr)
	}
}

func fixedTempName() (string, error) { return deterministicTempHex, nil }

func assertNoAtomicTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".senv-tmp-") {
			t.Errorf("temporary file was not cleaned up: %s", entry.Name())
		}
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("content of %q is incomplete: got %d bytes, want %d", path, len(got), len(want))
	}
}

func assertFileMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	if got := info.Mode().Perm(); got != want.Perm() {
		t.Fatalf("mode of %q = %04o, want %04o", path, got, want.Perm())
	}
}
