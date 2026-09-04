package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wii/senv/internal/securefs"
)

const diskCacheDirName = "senv"

// diskCacheStore is the explicit opt-in escape hatch for environments without
// a platform secure store (headless macOS, CI). It keeps the historical file
// hardening — 0700 directory, 0600 atomic no-follow writes, boot ID validation
// at the manager layer — but the key is stored unencrypted on disk.
type diskCacheStore struct{}

func diskCacheBase() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve disk cache home: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return base, nil
}

func (diskCacheStore) Save(cache *SessionCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}
	base, err := diskCacheBase()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return fmt.Errorf("failed to create disk cache base: %w", err)
	}
	root, err := securefs.OpenRoot(base)
	if err != nil {
		return fmt.Errorf("failed to open disk cache: %w", err)
	}
	defer root.Close()
	if err := root.EnsureDir([]string{diskCacheDirName}, 0o700); err != nil {
		return fmt.Errorf("failed to secure disk cache directory: %w", err)
	}
	if err := root.AtomicWrite([]string{diskCacheDirName, "session.json"}, data, 0o600); err != nil {
		return fmt.Errorf("failed to write disk cache: %w", err)
	}
	return nil
}

func (diskCacheStore) Load() (*SessionCache, error) {
	base, err := diskCacheBase()
	if err != nil {
		return nil, err
	}
	root, err := securefs.OpenRoot(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open disk cache: %w", err)
	}
	defer root.Close()
	data, err := root.Read(diskCacheDirName, "session.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read disk cache: %w", err)
	}
	var cache SessionCache
	// Corrupt escape-hatch files are treated as "no session" and deliberately
	// left in place for diagnosis rather than silently deleted.
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, nil
	}
	return &cache, nil
}

func (diskCacheStore) Clear() error {
	base, err := diskCacheBase()
	if err != nil {
		return err
	}
	root, err := securefs.OpenRoot(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to open disk cache: %w", err)
	}
	defer root.Close()
	if err := root.Remove(diskCacheDirName, "session.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove disk cache: %w", err)
	}
	return nil
}
