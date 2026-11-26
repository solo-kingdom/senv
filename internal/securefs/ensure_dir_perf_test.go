package securefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirCompliantAndTighten(t *testing.T) {
	base := t.TempDir()
	root, err := OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	if err := root.EnsureDir([]string{"compliant"}, 0o700); err != nil {
		t.Fatalf("initial EnsureDir: %v", err)
	}
	// Second EnsureDir on already-0700 dir should succeed and preserve 0700.
	if err := root.EnsureDir([]string{"compliant"}, 0o700); err != nil {
		t.Fatalf("second EnsureDir: %v", err)
	}
	info, err := os.Stat(filepath.Join(base, "compliant"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("compliant perm = %o, want 0700", perm)
	}

	// Pre-existing 0755 dir must still be tightened to 0700.
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := root.EnsureDir([]string{"loose"}, 0o700); err != nil {
		t.Fatalf("EnsureDir tighten loose: %v", err)
	}
	infoLoose, err := os.Stat(loose)
	if err != nil {
		t.Fatal(err)
	}
	if perm := infoLoose.Mode().Perm(); perm != 0o700 {
		t.Fatalf("tightened perm = %o, want 0700", perm)
	}
}
