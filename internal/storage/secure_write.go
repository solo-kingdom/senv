package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/securefs"
)

// WriteSensitiveFile atomically writes a private file without following a
// symlink at the target or its immediate parent. Storage-managed paths use
// Manager's trusted config/data roots directly; this helper remains for callers
// that already supply a complete filesystem path.
func WriteSensitiveFile(path string, data []byte, dirPerm, filePerm os.FileMode) error {
	parent := filepath.Dir(filepath.Clean(path))
	if err := EnsurePrivateDir(parent, dirPerm); err != nil {
		return err
	}
	root, err := securefs.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open sensitive-file parent: %w", err)
	}
	defer root.Close()
	if err := securefs.ValidateSegment(filepath.Base(path)); err != nil {
		return err
	}
	return root.AtomicWrite([]string{filepath.Base(path)}, data, filePerm)
}

// EnsurePrivateDir creates dir (and parents) with dirPerm and tightens an
// existing loose real directory. Every component is traversed relative to an
// opened filesystem-root descriptor, so neither an existing nor concurrently
// substituted intermediate symlink is followed.
func EnsurePrivateDir(dir string, dirPerm os.FileMode) error {
	if dirPerm&^os.FileMode(0o777) != 0 {
		return fmt.Errorf("private directory mode contains non-permission bits")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve private directory %q: %w", dir, err)
	}
	absolute = filepath.Clean(absolute)
	rootPath := filepath.VolumeName(absolute) + string(filepath.Separator)
	relative, err := filepath.Rel(rootPath, absolute)
	if err != nil || relative == "." || relative == "" || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("private directory %q is outside a usable filesystem root", dir)
	}
	segments := strings.Split(relative, string(filepath.Separator))
	root, err := securefs.OpenRoot(rootPath)
	if err != nil {
		return fmt.Errorf("open private-directory filesystem root: %w", err)
	}
	defer root.Close()
	if err := root.EnsureDir(segments, dirPerm.Perm()); err != nil {
		return fmt.Errorf("prepare private directory %q: %w", dir, err)
	}
	return nil
}
