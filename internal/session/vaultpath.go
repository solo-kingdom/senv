package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// normalizeVaultPath turns a data path into a stable vault identity: absolute,
// cleaned, and with the longest existing prefix resolved through symlinks. When
// the path (or its parents) do not exist yet, it falls back to the absolute
// cleaned form so init-time checks still work.
func normalizeVaultPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("data path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved, nil
	}
	resolvedDir, err := resolveExistingPrefix(filepath.Dir(absolute))
	if err != nil {
		return absolute, nil
	}
	return filepath.Join(resolvedDir, filepath.Base(absolute)), nil
}

// resolveExistingPrefix resolves the deepest existing ancestor of dir and
// re-appends the unresolved tail, so a not-yet-created vault directory keeps a
// stable identity across invocations.
func resolveExistingPrefix(dir string) (string, error) {
	var pending []string
	current := dir
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(append([]string{resolved}, pending...)...), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		pending = append([]string{filepath.Base(current)}, pending...)
		current = parent
	}
}

// vaultSlotFor returns the cache slot for a data path. It never fails: an
// unnormalizable path falls back to its raw bytes so callers still get a stable
// slot instead of losing the session entirely.
func vaultSlotFor(dataPath string) string {
	normalized, err := normalizeVaultPath(dataPath)
	if err != nil {
		normalized = dataPath
	}
	return hashDataPath(normalized)
}
