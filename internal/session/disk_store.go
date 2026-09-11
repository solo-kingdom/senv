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

// diskCacheLegacyFileName is the pre-slot single-cache file name.
const diskCacheLegacyFileName = "session.json"

func diskCacheFileName(slot string) string {
	return fmt.Sprintf("session-%s.json", slot)
}

// diskCacheStore is the disk escape hatch: explicit --insecure-cache on Linux/CI,
// and the Darwin default write target when no tmpfs/ramfs can be proven. It keeps
// 0700 directory, 0600 atomic no-follow writes, and boot ID validation at the
// manager layer, but the key is stored unencrypted on disk.
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

func (diskCacheStore) Save(slot string, cache *SessionCache) error {
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
	if err := root.AtomicWrite([]string{diskCacheDirName, diskCacheFileName(slot)}, data, 0o600); err != nil {
		return fmt.Errorf("failed to write disk cache: %w", err)
	}
	return nil
}

func (diskCacheStore) Load(slot string) (*SessionCache, error) {
	return loadDiskCacheFile(diskCacheFileName(slot))
}

func (diskCacheStore) LoadLegacy() (*SessionCache, error) {
	return loadDiskCacheFile(diskCacheLegacyFileName)
}

func loadDiskCacheFile(name string) (*SessionCache, error) {
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
	data, err := root.Read(diskCacheDirName, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read disk cache: %w", err)
	}
	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		// Corrupt escape-hatch files are reported as unverifiable and left in
		// place for diagnosis rather than silently deleted.
		return nil, fmt.Errorf("%w: corrupt escape-hatch session cache; run: senv session clear --all", ErrSessionUnverifiable)
	}
	return &cache, nil
}

func (diskCacheStore) Clear(slot string) error {
	return removeDiskCacheFile(diskCacheFileName(slot))
}

func (diskCacheStore) ClearLegacy() error {
	return removeDiskCacheFile(diskCacheLegacyFileName)
}

func (diskCacheStore) ClearAll() error {
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
	if err := root.RemoveTree(diskCacheDirName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove disk cache: %w", err)
	}
	return nil
}

func removeDiskCacheFile(name string) error {
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
	if err := root.Remove(diskCacheDirName, name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove disk cache: %w", err)
	}
	return nil
}
