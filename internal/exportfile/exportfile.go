// Package exportfile provides the shared security boundary for plaintext exports.
package exportfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wii/senv/internal/securefs"
)

const (
	DefaultMode fs.FileMode = 0o600
	ParentMode  fs.FileMode = 0o700
)

var octalModePattern = regexp.MustCompile(`^0[0-7]{3}$`)

// Path is a normalized export target represented relative to its filesystem
// volume root. Segments are validated before any filesystem access.
type Path struct {
	Absolute       string
	Root           string
	Segments       []string
	ParentSegments []string
	Base           string
}

// ParseFileMode accepts only four-digit ordinary octal permissions in the
// range 0000-0777. Special bits and symbolic/decimal forms are rejected.
func ParseFileMode(raw string) (fs.FileMode, error) {
	if !octalModePattern.MatchString(raw) {
		return 0, fmt.Errorf("invalid file mode %q: expected four octal digits from 0000 to 0777", raw)
	}
	var mode fs.FileMode
	for _, digit := range raw[1:] {
		mode = mode<<3 | fs.FileMode(digit-'0')
	}
	return mode, nil
}

// ResolvePath expands a leading ~/ and resolves basename, relative, and
// absolute paths without touching the filesystem.
func ResolvePath(raw string) (Path, error) {
	if raw == "" {
		return Path{}, fmt.Errorf("export path is empty")
	}

	expanded := raw
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Path{}, fmt.Errorf("resolve home directory: %w", err)
		}
		if raw == "~" {
			expanded = home
		} else {
			expanded = filepath.Join(home, raw[2:])
		}
	}

	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return Path{}, fmt.Errorf("resolve export path %q: %w", raw, err)
	}
	absolute = filepath.Clean(absolute)
	volume := filepath.VolumeName(absolute)
	rootPath := volume + string(filepath.Separator)
	relative, err := filepath.Rel(rootPath, absolute)
	if err != nil || relative == "." || relative == "" || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return Path{}, fmt.Errorf("invalid export path %q", raw)
	}

	segments := strings.Split(relative, string(filepath.Separator))
	for _, segment := range segments {
		if err := securefs.ValidateSegment(segment); err != nil {
			return Path{}, fmt.Errorf("invalid export path %q: %w", raw, err)
		}
	}
	parent := append([]string(nil), segments[:len(segments)-1]...)
	return Path{
		Absolute:       absolute,
		Root:           rootPath,
		Segments:       segments,
		ParentSegments: parent,
		Base:           segments[len(segments)-1],
	}, nil
}

type trustedRoot interface {
	Close() error
	ReadWithMode(segments ...string) ([]byte, fs.FileMode, error)
	EnsurePrivateParents(segments []string, mode fs.FileMode) error
	AtomicWrite(segments []string, data []byte, mode fs.FileMode) error
	Remove(segments ...string) error
}

var openRoot = func(path string) (trustedRoot, error) {
	return securefs.OpenRoot(path)
}

// ReadFile reads a regular export target and its permissions without following
// any target or parent symlink.
func ReadFile(path string) ([]byte, fs.FileMode, error) {
	resolved, err := ResolvePath(path)
	if err != nil {
		return nil, 0, err
	}
	root, err := openRoot(resolved.Root)
	if err != nil {
		return nil, 0, fmt.Errorf("open export root: %w", err)
	}
	defer root.Close()

	data, mode, err := root.ReadWithMode(resolved.Segments...)
	if err != nil {
		return nil, 0, fmt.Errorf("read export target %s: %w", resolved.Absolute, err)
	}
	return data, mode.Perm(), nil
}

// WriteFile creates only missing parent directories privately, then atomically
// writes the target without following symlinks. securefs preserves any
// existing mode that is stricter than requested.
func WriteFile(path string, data []byte, mode fs.FileMode) error {
	if mode&^fs.ModePerm != 0 {
		return fmt.Errorf("invalid file mode %04o: special bits are not supported", mode)
	}
	resolved, err := ResolvePath(path)
	if err != nil {
		return err
	}
	root, err := openRoot(resolved.Root)
	if err != nil {
		return fmt.Errorf("open export root: %w", err)
	}
	defer root.Close()

	if len(resolved.ParentSegments) > 0 {
		if err := root.EnsurePrivateParents(resolved.ParentSegments, ParentMode); err != nil {
			return fmt.Errorf("prepare export parent for %s: %w", resolved.Absolute, err)
		}
	}
	if err := root.AtomicWrite(resolved.Segments, data, mode.Perm()); err != nil {
		return fmt.Errorf("write export target %s: %w", resolved.Absolute, err)
	}
	return nil
}

// RemoveFile removes a regular plaintext target without following a target or
// parent symlink. The filesystem root handle anchors the full resolved path.
func RemoveFile(path string) error {
	resolved, err := ResolvePath(path)
	if err != nil {
		return err
	}
	root, err := openRoot(resolved.Root)
	if err != nil {
		return fmt.Errorf("open export root: %w", err)
	}
	defer root.Close()
	if err := root.Remove(resolved.Segments...); err != nil {
		return fmt.Errorf("remove export target %s: %w", resolved.Absolute, err)
	}
	return nil
}
