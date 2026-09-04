//go:build linux || darwin

package securefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveRegularFile(t *testing.T) {
	rootDir := t.TempDir()
	path := filepath.Join(rootDir, "entry")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, rootDir)

	if err := root.Remove("entry"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	assertNotExist(t, path)
}

func TestRemoveRejectsInvalidSegmentsWithoutMutation(t *testing.T) {
	rootDir := t.TempDir()
	keep := filepath.Join(rootDir, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, rootDir)

	for _, segments := range [][]string{{"..", "keep"}, {"bad/name"}, {`bad\name`}, {""}, {"/tmp"}} {
		if err := root.Remove(segments...); !errors.Is(err, ErrInvalidSegment) {
			t.Errorf("Remove(%q) error = %v, want ErrInvalidSegment", segments, err)
		}
	}
	if err := root.Remove(); !errors.Is(err, ErrContainment) {
		t.Errorf("Remove() error = %v, want ErrContainment", err)
	}
	assertFileContent(t, keep, "keep")
}

func TestRemoveRejectsTargetAndParentSymlinks(t *testing.T) {
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("target", func(t *testing.T) {
		rootDir := t.TempDir()
		link := filepath.Join(rootDir, "entry")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.Remove("entry"); !errors.Is(err, ErrSymlink) {
			t.Fatalf("Remove(target symlink) error = %v, want ErrSymlink", err)
		}
		assertSymlink(t, link)
	})

	t.Run("parent", func(t *testing.T) {
		rootDir := t.TempDir()
		if err := os.Symlink(outsideDir, filepath.Join(rootDir, "parent")); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.Remove("parent", "sentinel"); !errors.Is(err, ErrSymlink) {
			t.Fatalf("Remove(parent symlink) error = %v, want ErrSymlink", err)
		}
	})
	assertFileContent(t, outside, "outside")
}

func TestRenameRegularFileAcrossTrustedDirectories(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "from"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "to"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(rootDir, "from", "entry")
	destination := filepath.Join(rootDir, "to", "renamed")
	if err := os.WriteFile(source, []byte("complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, rootDir)

	if err := root.Rename([]string{"from", "entry"}, []string{"to", "renamed"}); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	assertNotExist(t, source)
	assertFileContent(t, destination, "complete")
}

func TestRenameRejectsInvalidAndSymlinkEndpoints(t *testing.T) {
	t.Run("invalid destination", func(t *testing.T) {
		rootDir := t.TempDir()
		source := filepath.Join(rootDir, "source")
		if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.Rename([]string{"source"}, []string{"..", "escaped"}); !errors.Is(err, ErrInvalidSegment) {
			t.Fatalf("Rename invalid destination error = %v, want ErrInvalidSegment", err)
		}
		assertFileContent(t, source, "source")
	})

	t.Run("source symlink", func(t *testing.T) {
		rootDir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(rootDir, "source")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.Rename([]string{"source"}, []string{"destination"}); !errors.Is(err, ErrSymlink) {
			t.Fatalf("Rename source symlink error = %v, want ErrSymlink", err)
		}
		assertSymlink(t, link)
		assertFileContent(t, outside, "outside")
	})

	t.Run("destination symlink", func(t *testing.T) {
		rootDir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(rootDir, "source")
		if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(rootDir, "destination")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.Rename([]string{"source"}, []string{"destination"}); !errors.Is(err, ErrSymlink) {
			t.Fatalf("Rename destination symlink error = %v, want ErrSymlink", err)
		}
		assertFileContent(t, source, "source")
		assertSymlink(t, link)
		assertFileContent(t, outside, "outside")
	})

	t.Run("destination parent symlink", func(t *testing.T) {
		rootDir := t.TempDir()
		outsideDir := t.TempDir()
		outside := filepath.Join(outsideDir, "sentinel")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(rootDir, "source")
		if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideDir, filepath.Join(rootDir, "linked")); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.Rename([]string{"source"}, []string{"linked", "sentinel"}); !errors.Is(err, ErrSymlink) {
			t.Fatalf("Rename destination parent symlink error = %v, want ErrSymlink", err)
		}
		assertFileContent(t, source, "source")
		assertFileContent(t, outside, "outside")
	})
}

func TestRemoveTreeDeletesValidatedTree(t *testing.T) {
	rootDir := t.TempDir()
	tree := filepath.Join(rootDir, "group")
	if err := os.MkdirAll(filepath.Join(tree, "nested", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(tree, "a.enc"),
		filepath.Join(tree, "nested", "b.enc"),
		filepath.Join(tree, "nested", "deep", "c.enc"),
	} {
		if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := openTestRoot(t, rootDir)

	if err := root.RemoveTree("group"); err != nil {
		t.Fatalf("RemoveTree: %v", err)
	}
	assertNotExist(t, tree)
}

func TestRemoveTreeInvalidSegmentHasZeroSideEffects(t *testing.T) {
	rootDir := t.TempDir()
	tree := filepath.Join(rootDir, "group")
	if err := os.Mkdir(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(tree, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(rootDir), "outside-remove-tree-sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	root := openTestRoot(t, rootDir)

	if err := root.RemoveTree("..", filepath.Base(outside)); !errors.Is(err, ErrInvalidSegment) {
		t.Fatalf("RemoveTree traversal error = %v, want ErrInvalidSegment", err)
	}
	assertFileContent(t, keep, "keep")
	assertFileContent(t, outside, "outside")
}

func TestRemoveTreeNestedSymlinkHasZeroSideEffects(t *testing.T) {
	rootDir := t.TempDir()
	tree := filepath.Join(rootDir, "group")
	if err := os.MkdirAll(filepath.Join(tree, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	keepTop := filepath.Join(tree, "a-keep")
	keepNested := filepath.Join(tree, "nested", "keep")
	if err := os.WriteFile(keepTop, []byte("top"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepNested, []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree, "nested", "z-link")); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, rootDir)

	if err := root.RemoveTree("group"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("RemoveTree nested symlink error = %v, want ErrSymlink", err)
	}
	assertFileContent(t, keepTop, "top")
	assertFileContent(t, keepNested, "nested")
	assertFileContent(t, outside, "outside")
}

func TestRemoveTreePollutedIdentityHasZeroSideEffects(t *testing.T) {
	rootDir := t.TempDir()
	tree := filepath.Join(rootDir, "group")
	if err := os.Mkdir(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(tree, "a-keep")
	polluted := filepath.Join(tree, "bad:name")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(polluted, []byte("polluted"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, rootDir)

	if err := root.RemoveTree("group"); !errors.Is(err, ErrInvalidSegment) {
		t.Fatalf("RemoveTree polluted identity error = %v, want ErrInvalidSegment", err)
	}
	assertFileContent(t, keep, "keep")
	assertFileContent(t, polluted, "polluted")
}

func TestRemoveTreeRejectsTargetAndParentSymlinks(t *testing.T) {
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("target", func(t *testing.T) {
		rootDir := t.TempDir()
		link := filepath.Join(rootDir, "group")
		if err := os.Symlink(outsideDir, link); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.RemoveTree("group"); !errors.Is(err, ErrSymlink) {
			t.Fatalf("RemoveTree target symlink error = %v, want ErrSymlink", err)
		}
		assertSymlink(t, link)
	})

	t.Run("parent", func(t *testing.T) {
		rootDir := t.TempDir()
		if err := os.Symlink(outsideDir, filepath.Join(rootDir, "linked")); err != nil {
			t.Fatal(err)
		}
		root := openTestRoot(t, rootDir)
		if err := root.RemoveTree("linked", "child"); !errors.Is(err, ErrSymlink) {
			t.Fatalf("RemoveTree parent symlink error = %v, want ErrSymlink", err)
		}
	})
	assertFileContent(t, outside, "outside")
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%q still exists or could not be checked: %v", path, err)
	}
}

func assertSymlink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%q mode = %v, want symlink", path, info.Mode())
	}
}

func TestRemoveTreeConcurrentReplacementHasZeroOriginalDeletion(t *testing.T) {
	rootDir := t.TempDir()
	tree := filepath.Join(rootDir, "group")
	if err := os.Mkdir(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(tree, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, rootDir)
	entered := make(chan struct{})
	resume := make(chan struct{})
	removeTreeBeforeQuarantine = func() {
		close(entered)
		<-resume
	}
	t.Cleanup(func() { removeTreeBeforeQuarantine = nil })
	done := make(chan error, 1)
	go func() { done <- root.RemoveTree("group") }()
	<-entered
	original := filepath.Join(rootDir, "group-original")
	if err := os.Rename(tree, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := <-done; !errors.Is(err, ErrRootChanged) {
		t.Fatalf("RemoveTree error = %v, want ErrRootChanged", err)
	}
	assertFileContent(t, filepath.Join(original, "keep"), "keep")
}
