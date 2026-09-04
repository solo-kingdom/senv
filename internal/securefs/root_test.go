//go:build linux || darwin

package securefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNoFollowTargetSymlink(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(rootDir, "entry")); err != nil {
		t.Fatal(err)
	}

	root := openTestRoot(t, rootDir)
	if data, err := root.Read("entry"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Read(target symlink) = %q, %v; want ErrSymlink", data, err)
	}
	assertFileContent(t, outside, "outside-secret")
}

func TestNoFollowIntermediateSymlink(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(rootDir, "linked-parent")); err != nil {
		t.Fatal(err)
	}

	root := openTestRoot(t, rootDir)
	if data, err := root.Read("linked-parent", "sentinel"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Read(parent symlink) = %q, %v; want ErrSymlink", data, err)
	}
	assertFileContent(t, outside, "outside-secret")
}

func TestContainmentRejectsEscapingSegments(t *testing.T) {
	rootDir := t.TempDir()
	outside := filepath.Join(filepath.Dir(rootDir), "outside-sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	root := openTestRoot(t, rootDir)
	attacks := [][]string{
		{"..", filepath.Base(outside)},
		{"subdir/../../" + filepath.Base(outside)},
		{"/", "tmp"},
		{`C:\`, "outside"},
	}
	for _, attack := range attacks {
		if data, err := root.Read(attack...); !errors.Is(err, ErrInvalidSegment) {
			t.Errorf("Read(%q) = %q, %v; want ErrInvalidSegment", attack, data, err)
		}
	}
	if data, err := root.Read(); !errors.Is(err, ErrContainment) {
		t.Errorf("Read() = %q, %v; want ErrContainment", data, err)
	}
	assertFileContent(t, outside, "outside")
}

func TestRootReplacementRemainsAnchored(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "root")
	anchoredPath := filepath.Join(parent, "anchored")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "value"), []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, rootPath)

	if err := os.Rename(rootPath, anchoredPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "value"), []byte("attacker"), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := root.Read("value")
	if err != nil {
		t.Fatalf("Read after root replacement: %v", err)
	}
	if string(data) != "trusted" {
		t.Fatalf("Read after root replacement = %q, want anchored trusted value", data)
	}
	assertFileContent(t, filepath.Join(rootPath, "value"), "attacker")
}

func TestRootRejectsSymlink(t *testing.T) {
	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(link)
	if root != nil || !errors.Is(err, ErrSymlink) {
		t.Fatalf("OpenRoot(symlink) = %#v, %v; want nil, ErrSymlink", root, err)
	}
}

func TestRootUnsupportedBackendContract(t *testing.T) {
	err := unsupportedPlatformError("open root", "/vault")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported error = %v, want ErrUnsupported", err)
	}
	var pathErr *PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "open root" {
		t.Fatalf("unsupported error = %#v, want typed open-root PathError", err)
	}
}

func openTestRoot(t *testing.T, path string) *Root {
	t.Helper()
	root, err := OpenRoot(path)
	if err != nil {
		t.Fatalf("OpenRoot(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("Close root: %v", err)
		}
	})
	return root
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("content of %q = %q, want %q", path, data, want)
	}
}
