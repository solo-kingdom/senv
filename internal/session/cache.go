package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/wii/senv/internal/securefs"
)

const fallbackDirRandomBytes = 16

// cacheLocation describes a cache relative to an already validated runtime
// filesystem root. Keeping path segments separate lets securefs reject links.
type cacheLocation struct {
	root     string
	segments []string
	fallback string
}

func cacheFileName() string {
	return fmt.Sprintf("session-%d", os.Getuid())
}

func fallbackDirPrefix() string {
	return fmt.Sprintf("senv-%d-", os.Getuid())
}

func legacyPersistentCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "senv", "session", cacheFileName())
}

// removeLegacyPersistentCache removes only through a trusted home-directory
// handle. A symlink in any managed component causes a fail-closed best-effort
// cleanup rather than traversal outside HOME.
func removeLegacyPersistentCache() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	root, err := securefs.OpenRoot(home)
	if err != nil {
		return
	}
	defer root.Close()
	err = root.Remove(".local", "share", "senv", "session", cacheFileName())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
}

// randRead is the single cryptographic-randomness seam used by session IDs and
// fallback directory names. Tests may inject failures; production never does.
var randRead = rand.Read

func randomHex(byteCount int, purpose string) (string, error) {
	value := make([]byte, byteCount)
	n, err := randRead(value)
	if err != nil {
		return "", fmt.Errorf("failed to read random bytes for %s: %w", purpose, err)
	}
	if n != len(value) {
		return "", fmt.Errorf("failed to read random bytes for %s: %w", purpose, io.ErrUnexpectedEOF)
	}
	return hex.EncodeToString(value), nil
}

func generateSessionID() (string, error) {
	value, err := randomHex(16, "session id")
	if err != nil {
		return "", err
	}
	return "sess-" + value, nil
}

func generateFallbackDirName() (string, error) {
	value, err := randomHex(fallbackDirRandomBytes, "session cache directory")
	if err != nil {
		return "", err
	}
	return fallbackDirPrefix() + value, nil
}

func hashDataPath(dataPath string) string {
	hash := sha256.Sum256([]byte(dataPath))
	return hex.EncodeToString(hash[:16])
}

// rejectSymlinkComponents rejects an environment-provided runtime path if any
// existing component is a link. securefs then anchors later operations to the
// opened final directory handle.
func rejectSymlinkComponents(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := filepath.VolumeName(absolute) + string(filepath.Separator)
	remainder := strings.TrimPrefix(absolute, current)
	for _, component := range strings.Split(remainder, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("session runtime path %q contains a symbolic link", current)
		}
	}
	return nil
}

func validateRuntimeRoot(path string) error {
	if path == "" {
		return fmt.Errorf("session runtime path is empty")
	}
	if err := rejectSymlinkComponents(path); err != nil {
		return err
	}
	if err := requireMemoryBackedFilesystem(path); err != nil {
		return err
	}
	root, err := securefs.OpenRoot(path)
	if err != nil {
		return err
	}
	return root.Close()
}

func xdgCacheLocation(create bool) (cacheLocation, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if err := validateRuntimeRoot(runtimeDir); err != nil {
		return cacheLocation{}, err
	}
	location := cacheLocation{root: runtimeDir, segments: []string{"senv", cacheFileName()}}
	if !create {
		return location, nil
	}
	root, err := securefs.OpenRoot(runtimeDir)
	if err != nil {
		return cacheLocation{}, err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{"senv"}, 0o700); err != nil {
		return cacheLocation{}, err
	}
	return location, nil
}

func fallbackDirectoryOwnedAndPrivate(info os.FileInfo) bool {
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func discoverFallbackLocations() ([]cacheLocation, error) {
	tempRoot := os.TempDir()
	if err := validateRuntimeRoot(tempRoot); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return nil, err
	}
	locations := make([]cacheLocation, 0, 1)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), fallbackDirPrefix()) {
			continue
		}
		if err := securefs.ValidateSegment(entry.Name()); err != nil {
			continue
		}
		info, err := os.Lstat(filepath.Join(tempRoot, entry.Name()))
		if err != nil || !fallbackDirectoryOwnedAndPrivate(info) {
			continue
		}
		locations = append(locations, cacheLocation{
			root: tempRoot, segments: []string{entry.Name(), cacheFileName()}, fallback: entry.Name(),
		})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].fallback < locations[j].fallback })
	return locations, nil
}

func newFallbackLocation() (cacheLocation, error) {
	tempRoot := os.TempDir()
	// The actual backing filesystem is checked before randomness or mkdir, and
	// therefore before any candidate directory can be written.
	if err := validateRuntimeRoot(tempRoot); err != nil {
		return cacheLocation{}, err
	}
	name, err := generateFallbackDirName()
	if err != nil {
		return cacheLocation{}, err
	}
	root, err := securefs.OpenRoot(tempRoot)
	if err != nil {
		return cacheLocation{}, err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{name}, 0o700); err != nil {
		return cacheLocation{}, err
	}
	return cacheLocation{root: tempRoot, segments: []string{name, cacheFileName()}, fallback: name}, nil
}

func locationsForRead() ([]cacheLocation, error) {
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		location, err := xdgCacheLocation(false)
		if err != nil {
			return nil, err
		}
		return []cacheLocation{location}, nil
	}
	return discoverFallbackLocations()
}

func locationForWrite() (cacheLocation, error) {
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		return xdgCacheLocation(true)
	}
	return newFallbackLocation()
}

func readLocation(location cacheLocation) (*SessionCache, bool, error) {
	root, err := securefs.OpenRoot(location.root)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	data, err := root.Read(location.segments...)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal cache: %w", err)
	}
	return &cache, true, nil
}

func saveCache(cache *SessionCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		return withFallbackLifecycleLock(tempRoot, func() error {
			return saveCacheAt(data, true)
		})
	}
	return saveCacheAt(data, false)
}

func saveCacheAt(data []byte, fallback bool) error {
	location, err := locationForWrite()
	if err != nil {
		return fmt.Errorf("failed to resolve secure session runtime: %w", err)
	}
	if fallback && location.fallback == "" {
		return fmt.Errorf("fallback session runtime resolved an XDG cache")
	}
	root, err := securefs.OpenRoot(location.root)
	if err != nil {
		return fmt.Errorf("failed to open session runtime: %w", err)
	}
	if err := root.AtomicWrite(location.segments, data, 0o600); err != nil {
		root.Close()
		if location.fallback != "" {
			cleanupFallbackDirectory(location.root, location.fallback)
		}
		return fmt.Errorf("failed to write session cache: %w", err)
	}
	if err := root.Close(); err != nil {
		return err
	}
	if location.fallback != "" {
		cleanupOtherFallbackDirectories(location.fallback)
	}
	removeLegacyRuntimeCache()
	removeLegacyPersistentCache()
	return nil
}

func loadCache() (*SessionCache, error) {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		var cache *SessionCache
		err := withFallbackLifecycleLock(tempRoot, func() error {
			var err error
			cache, err = loadCacheAt()
			return err
		})
		return cache, err
	}
	return loadCacheAt()
}

func loadCacheAt() (*SessionCache, error) {
	locations, err := locationsForRead()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve secure session runtime: %w", err)
	}
	var found *SessionCache
	for _, location := range locations {
		cache, exists, err := readLocation(location)
		if err != nil {
			return nil, fmt.Errorf("failed to read cache file: %w", err)
		}
		if !exists {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple session caches found in fallback runtime; clear the session")
		}
		found = cache
	}
	return found, nil
}

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

func cleanupFallbackDirectory(tempRoot, name string) {
	root, err := securefs.OpenRoot(tempRoot)
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.RemoveTree(name)
}

func cleanupOtherFallbackDirectories(keep string) {
	locations, err := discoverFallbackLocations()
	if err != nil {
		return
	}
	for _, location := range locations {
		if location.fallback != keep {
			cleanupFallbackDirectory(location.root, location.fallback)
		}
	}
}

func removeLegacyRuntimeCache() {
	tempRoot := os.TempDir()
	if err := validateRuntimeRoot(tempRoot); err != nil {
		return
	}
	name := fmt.Sprintf("senv-%d", os.Getuid())
	root, err := securefs.OpenRoot(tempRoot)
	if err != nil {
		return
	}
	defer root.Close()
	if err := root.RemoveTree(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
}

func clearCache() error {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		return withFallbackLifecycleLock(tempRoot, clearCacheAt)
	}
	return clearCacheAt()
}

func clearCacheAt() error {
	locations, err := locationsForRead()
	if err != nil {
		return fmt.Errorf("failed to resolve secure session runtime: %w", err)
	}
	for _, location := range locations {
		root, err := securefs.OpenRoot(location.root)
		if err != nil {
			return err
		}
		if location.fallback != "" {
			err = root.RemoveTree(location.fallback)
		} else {
			err = root.Remove(location.segments...)
		}
		closeErr := root.Close()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to remove cache file: %w", err)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	removeLegacyRuntimeCache()
	removeLegacyPersistentCache()
	return nil
}

// getCachePath is retained for same-package diagnostics/tests. It never creates
// a directory and therefore cannot weaken the pre-write validation policy.
func getCachePath() (string, error) {
	locations, err := locationsForRead()
	if err != nil {
		return "", err
	}
	if len(locations) == 0 {
		return "", os.ErrNotExist
	}
	return filepath.Join(append([]string{locations[0].root}, locations[0].segments...)...), nil
}
