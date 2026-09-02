package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// cacheFileName returns the session cache file name for the current user.
func cacheFileName() string {
	return fmt.Sprintf("session-%d", os.Getuid())
}

// legacyPersistentCachePath returns where versions before the tmpfs-only
// policy stored the "never" session cache (~/.local/share/senv/session).
// Writing there is forbidden now — it survives reboot and is swept up by
// home-directory backups — but new versions clean up files left behind.
func legacyPersistentCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "senv", "session", cacheFileName())
}

// removeLegacyPersistentCache deletes the persistent session cache written by
// older versions. Best effort: absence or removal failures must not block a
// fresh session.
func removeLegacyPersistentCache() {
	if path := legacyPersistentCachePath(); path != "" {
		os.Remove(path)
	}
}

// getCacheDir returns the user-private directory holding the session cache.
// The cache contains the base64-encoded derived key, so it MUST live on
// tmpfs (XDG_RUNTIME_DIR) only, for every timeout mode.
func getCacheDir() (string, error) {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		dir := filepath.Join(runtimeDir, "senv")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		return dir, nil
	}

	// XDG_RUNTIME_DIR unset (cron, containers, ...): fall back to a per-uid
	// directory under /tmp. An existing directory is verified to be private,
	// so a pre-created or tampered directory cannot hijack the cache.
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("senv-%d", os.Getuid()))
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("session cache directory %s exists but is not a private 0700 directory; refusing to store the session key there", dir)
	}
	return dir, nil
}

// getCachePath returns the cache file path inside the private cache dir.
func getCachePath() (string, error) {
	dir, err := getCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheFileName()), nil
}

// randRead is the randomness source for session identifiers. A variable so
// tests can inject failures; production code must never replace it.
var randRead = rand.Read

// generateSessionID generates a unique session ID. It fails closed when the
// randomness source is unavailable instead of falling back to a predictable
// identifier.
func generateSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := randRead(bytes); err != nil {
		return "", fmt.Errorf("failed to read random bytes for session id: %w", err)
	}
	return fmt.Sprintf("sess-%s", hex.EncodeToString(bytes)[:16]), nil
}

// hashDataPath creates a hash of the data path for validation
func hashDataPath(dataPath string) string {
	hash := sha256.Sum256([]byte(dataPath))
	return hex.EncodeToString(hash[:16])
}

// saveCache saves the session cache to disk.
//
// The file is created exclusively (O_EXCL, 0600) inside a 0700 private
// directory, refusing to follow symlinks; a leftover cache from a previous
// session is removed first — safe because only the current user can reach
// into the private directory. Legacy persistent caches are cleaned up so
// installations of older versions migrate on the next session start.
func saveCache(cache *SessionCache) error {
	cachePath, err := getCachePath()
	if err != nil {
		return fmt.Errorf("failed to resolve session cache path: %w", err)
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove previous cache file: %w", err)
	}

	f, err := os.OpenFile(cachePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("failed to write cache file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	removeLegacyPersistentCache()
	return nil
}

// readCacheFile reads and parses a session cache from the given path.
func readCacheFile(cachePath string) (*SessionCache, error) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return &cache, nil
}

// loadCache loads the session cache from disk. Returns (nil, nil) when no
// cache file exists.
func loadCache() (*SessionCache, error) {
	cachePath, err := getCachePath()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve session cache path: %w", err)
	}

	cache, err := readCacheFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	return cache, nil
}

// loadCacheForDataPath loads the session cache only if it was created for the
// given data path. Returns (nil, nil) when no cache exists for it.
func loadCacheForDataPath(dataPath string) (*SessionCache, error) {
	cache, err := loadCache()
	if err != nil || cache == nil {
		return nil, err
	}

	if cache.DataPathHash != hashDataPath(dataPath) {
		return nil, nil
	}

	return cache, nil
}

// clearCache removes the session cache file (and any legacy persistent one).
func clearCache() error {
	cachePath, err := getCachePath()
	if err != nil {
		return fmt.Errorf("failed to resolve session cache path: %w", err)
	}

	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}

	removeLegacyPersistentCache()
	return nil
}
