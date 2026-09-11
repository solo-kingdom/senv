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

// legacyNoticeFileName marks that the "unmatched legacy cache" hint was already
// shown, so the hint appears at most once per boot.
const legacyNoticeFileName = "legacy-cache-notice"

const legacyCacheNoticeMessage = "senv: 发现与当前 vault 不匹配的旧单槽会话缓存，已保留未删；" +
	"如需清理请运行 `senv session clear --all`"

// cacheLocation describes a cache relative to an already validated runtime
// filesystem root. Keeping path segments separate lets securefs reject links.
type cacheLocation struct {
	root     string
	segments []string
	fallback string
}

// cacheFileName is the per-vault slot file name: session-<uid>-<slot>.
func cacheFileName(slot string) string {
	return fmt.Sprintf("session-%d-%s", os.Getuid(), slot)
}

// legacyCacheFileName is the pre-slot single-cache file name.
func legacyCacheFileName() string {
	return fmt.Sprintf("session-%d", os.Getuid())
}

func fallbackDirPrefix() string {
	return fmt.Sprintf("senv-%d-", os.Getuid())
}

func fallbackDirPrefixForSlot(slot string) string {
	return fmt.Sprintf("senv-%d-%s-", os.Getuid(), slot)
}

func legacyPersistentCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "senv", "session", legacyCacheFileName())
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
	err = root.Remove(".local", "share", "senv", "session", legacyCacheFileName())
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

func generateFallbackDirName(slot string) (string, error) {
	value, err := randomHex(fallbackDirRandomBytes, "session cache directory")
	if err != nil {
		return "", err
	}
	return fallbackDirPrefixForSlot(slot) + value, nil
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

// resolveRuntimeRoot resolves trusted symlinks in the parent components of
// path (for example macOS /var -> /private/var) while keeping the final
// component unresolved so symlinked runtime directories stay rejected.
func resolveRuntimeRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(absolute)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("failed to resolve session runtime parent %q: %w", parent, err)
	}
	return filepath.Join(resolvedParent, filepath.Base(absolute)), nil
}

// validateRuntimeRoot checks that the resolved runtime path has no symlink
// components and is memory-backed. It returns the resolved root so later
// operations anchor to the same directory that was validated.
func validateRuntimeRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("session runtime path is empty")
	}
	resolved, err := resolveRuntimeRoot(path)
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkComponents(resolved); err != nil {
		return "", err
	}
	if err := requireMemoryBackedFilesystem(resolved); err != nil {
		return "", err
	}
	root, err := securefs.OpenRoot(resolved)
	if err != nil {
		return "", err
	}
	return resolved, root.Close()
}

func xdgCacheLocation(slot string, create bool) (cacheLocation, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	runtimeRoot, err := validateRuntimeRoot(runtimeDir)
	if err != nil {
		return cacheLocation{}, err
	}
	location := cacheLocation{root: runtimeRoot, segments: []string{"senv", cacheFileName(slot)}}
	if !create {
		return location, nil
	}
	root, err := securefs.OpenRoot(runtimeRoot)
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

// discoverFallbackLocations returns the slot files for one vault across all
// owned private fallback directories.
func discoverFallbackLocations(slot string) ([]cacheLocation, error) {
	tempRoot := os.TempDir()
	tempRoot, err := validateRuntimeRoot(tempRoot)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return nil, err
	}
	prefix := fallbackDirPrefixForSlot(slot)
	locations := make([]cacheLocation, 0, 1)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
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
			root: tempRoot, segments: []string{entry.Name(), cacheFileName(slot)}, fallback: entry.Name(),
		})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].fallback < locations[j].fallback })
	return locations, nil
}

// discoverAllFallbackDirectories returns every fallback directory owned by this
// user, including pre-slot ones (old random names without a slot segment).
func discoverAllFallbackDirectories() ([]cacheLocation, error) {
	tempRoot := os.TempDir()
	tempRoot, err := validateRuntimeRoot(tempRoot)
	if err != nil {
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
		locations = append(locations, cacheLocation{root: tempRoot, fallback: entry.Name()})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].fallback < locations[j].fallback })
	return locations, nil
}

// discoverLegacyFallbackLocations returns pre-slot fallback directories that
// may still hold a legacy single-cache file.
func discoverLegacyFallbackLocations() ([]cacheLocation, error) {
	all, err := discoverAllFallbackDirectories()
	if err != nil {
		return nil, err
	}
	legacy := make([]cacheLocation, 0, len(all))
	for _, location := range all {
		if isSlotFallbackDirName(location.fallback) {
			continue
		}
		legacy = append(legacy, cacheLocation{
			root:     location.root,
			segments: []string{location.fallback, legacyCacheFileName()},
			fallback: location.fallback,
		})
	}
	return legacy, nil
}

// isSlotFallbackDirName reports whether a fallback directory belongs to a
// per-vault slot (name shape senv-<uid>-<slot16hex>-<random>) rather than the
// pre-slot single-cache naming.
func isSlotFallbackDirName(name string) bool {
	rest, ok := strings.CutPrefix(name, fallbackDirPrefix())
	if !ok {
		return false
	}
	slot, tail, ok := strings.Cut(rest, "-")
	if !ok || tail == "" || len(slot) != 16 {
		return false
	}
	_, err := hex.DecodeString(slot)
	return err == nil
}

func newFallbackLocation(slot string) (cacheLocation, error) {
	tempRoot := os.TempDir()
	// The actual backing filesystem is checked before randomness or mkdir, and
	// therefore before any candidate directory can be written.
	tempRoot, err := validateRuntimeRoot(tempRoot)
	if err != nil {
		return cacheLocation{}, err
	}
	name, err := generateFallbackDirName(slot)
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
	return cacheLocation{root: tempRoot, segments: []string{name, cacheFileName(slot)}, fallback: name}, nil
}

func locationsForRead(slot string) ([]cacheLocation, error) {
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		location, err := xdgCacheLocation(slot, false)
		if err != nil {
			return nil, err
		}
		return []cacheLocation{location}, nil
	}
	return discoverFallbackLocations(slot)
}

func locationForWrite(slot string) (cacheLocation, error) {
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		return xdgCacheLocation(slot, true)
	}
	return newFallbackLocation(slot)
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

func saveTmpfsCache(slot string, cache *SessionCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		return withFallbackLifecycleLock(tempRoot, func() error {
			return saveCacheAt(data, slot, true)
		})
	}
	return saveCacheAt(data, slot, false)
}

func saveCacheAt(data []byte, slot string, fallback bool) error {
	location, err := locationForWrite(slot)
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
		cleanupOtherFallbackDirectories(slot, location.fallback)
	}
	return nil
}

func loadTmpfsCache(slot string) (*SessionCache, error) {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		var cache *SessionCache
		err := withFallbackLifecycleLock(tempRoot, func() error {
			var err error
			cache, err = loadCacheAt(slot)
			return err
		})
		return cache, err
	}
	return loadCacheAt(slot)
}

func loadCacheAt(slot string) (*SessionCache, error) {
	locations, err := locationsForRead(slot)
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
			return nil, fmt.Errorf("%w: multiple session caches found for this vault; run: senv session clear --all", ErrSessionUnverifiable)
		}
		found = cache
	}
	return found, nil
}

// loadLegacyTmpfsCache reads a pre-slot single cache, if any.
func loadLegacyTmpfsCache() (*SessionCache, error) {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		var cache *SessionCache
		err := withFallbackLifecycleLock(tempRoot, func() error {
			locations, err := discoverLegacyFallbackLocations()
			if err != nil {
				return err
			}
			for _, location := range locations {
				found, exists, err := readLocation(location)
				if err != nil {
					return fmt.Errorf("failed to read legacy cache file: %w", err)
				}
				if !exists {
					continue
				}
				if cache != nil {
					return fmt.Errorf("%w: multiple legacy session caches found; run: senv session clear --all", ErrSessionUnverifiable)
				}
				cache = found
			}
			return nil
		})
		return cache, err
	}
	runtimeRoot, err := validateRuntimeRoot(os.Getenv("XDG_RUNTIME_DIR"))
	if err != nil {
		return nil, err
	}
	cache, exists, err := readLocation(cacheLocation{root: runtimeRoot, segments: []string{"senv", legacyCacheFileName()}})
	if err != nil {
		return nil, fmt.Errorf("failed to read legacy cache file: %w", err)
	}
	if !exists {
		return nil, nil
	}
	return cache, nil
}

func clearLegacyTmpfsCache() error {
	var errs []error
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		err := withFallbackLifecycleLock(tempRoot, func() error {
			locations, derr := discoverLegacyFallbackLocations()
			if derr != nil {
				return derr
			}
			for _, location := range locations {
				cleanupFallbackDirectory(location.root, location.fallback)
			}
			return nil
		})
		return errors.Join(append(errs, err)...)
	}
	runtimeRoot, err := validateRuntimeRoot(os.Getenv("XDG_RUNTIME_DIR"))
	if err != nil {
		return err
	}
	root, err := securefs.OpenRoot(runtimeRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove("senv", legacyCacheFileName()); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("failed to remove legacy cache file: %w", err))
	}
	return errors.Join(errs...)
}

// loadCacheForDataPath loads the cache bound to dataPath's normalized vault slot.
func loadCacheForDataPath(dataPath string) (*SessionCache, error) {
	return loadCache(vaultSlotFor(dataPath))
}

func loadCache(slot string) (*SessionCache, error) {
	primary, primaryErr := activeSessionStoreFor(slot).Load(slot)
	hatch, hatchErr := (diskCacheStore{}).Load(slot)

	if primaryErr == nil && hatchErr == nil {
		switch {
		case primary != nil && hatch != nil:
			// Deterministic selection instead of "multiple caches" hard-fail
			// (ADR-0017): prefer the newer cache so an upgrade or an explicit
			// escape hatch does not strand the user. Both entries stay on disk
			// and whichever one is picked must still pass full validation.
			return selectNewerCache(slot, primary, hatch)
		case primary != nil:
			return primary, nil
		case hatch != nil:
			return hatch, nil
		default:
			return adoptLegacyCache(slot)
		}
	}
	if primaryErr != nil {
		// A locked/unavailable platform store must not strand a usable
		// escape-hatch session, but without one the actionable error stays.
		if hatch != nil {
			markHatchCacheSelected()
			return hatch, nil
		}
		return nil, errors.Join(primaryErr, hatchErr)
	}
	if primary != nil {
		return primary, nil
	}
	return nil, hatchErr
}

// selectNewerCache resolves two readable caches for one slot. The newer
// created_at wins; an exact tie is ambiguous and reported as an actionable
// error. Neither cache is deleted here: the ignored one may be the only
// recovery key for another vault slot. Preferring the less secure disk hatch
// over a readable primary is a downgrade, so it is not silent: the first
// occurrence warns on stderr and leaves an audit-visible flag.
func selectNewerCache(slot string, primary, hatch *SessionCache) (*SessionCache, error) {
	switch {
	case primary.CreatedAt.After(hatch.CreatedAt):
		return primary, nil
	case hatch.CreatedAt.After(primary.CreatedAt):
		markHatchCacheSelected()
		return hatch, nil
	default:
		return nil, errMultipleSessionCaches
	}
}

// adoptLegacyCache promotes a pre-slot single cache whose data path hash
// matches this vault. Unmatched legacy caches are left untouched so they can
// still serve as recovery keys, and the user is told once how to clear them.
func adoptLegacyCache(slot string) (*SessionCache, error) {
	store := activeSessionStoreFor(slot)
	if legacy, err := store.LoadLegacy(); err != nil {
		return nil, err
	} else if legacy != nil {
		return adoptLegacyEntry(store, slot, legacy)
	}
	if legacy, err := (diskCacheStore{}).LoadLegacy(); err != nil {
		return nil, err
	} else if legacy != nil {
		return adoptLegacyEntry(diskCacheStore{}, slot, legacy)
	}
	return nil, nil
}

func adoptLegacyEntry(store SessionStore, slot string, legacy *SessionCache) (*SessionCache, error) {
	if legacy.DataPathHash != slot {
		notifyLegacyCacheOnce()
		return nil, nil
	}
	if err := saveCache(slot, legacy); err != nil {
		return nil, err
	}
	_ = store.ClearLegacy()
	return legacy, nil
}

// notifyLegacyCacheOnce writes a marker in the runtime directory so the hint is
// shown at most once per boot.
func notifyLegacyCacheOnce() {
	root, segments, ok := legacyNoticeLocation()
	if !ok {
		return
	}
	handle, err := securefs.OpenRoot(root)
	if err != nil {
		return
	}
	defer handle.Close()
	if _, err := handle.Read(segments...); err == nil {
		return
	}
	if len(segments) > 1 {
		_ = handle.EnsureDir(segments[:len(segments)-1], 0o700)
	}
	if err := handle.AtomicWrite(segments, []byte("shown\n"), 0o600); err != nil {
		return
	}
	fmt.Fprintln(os.Stderr, legacyCacheNoticeMessage)
}

func legacyNoticeLocation() (string, []string, bool) {
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		root, err := validateRuntimeRoot(os.Getenv("XDG_RUNTIME_DIR"))
		if err != nil {
			return "", nil, false
		}
		return root, []string{"senv", legacyNoticeFileName}, true
	}
	root, err := validateRuntimeRoot(os.TempDir())
	if err != nil {
		return "", nil, false
	}
	return root, []string{fmt.Sprintf(".senv-legacy-notice-%d", os.Getuid())}, true
}

func cleanupFallbackDirectory(tempRoot, name string) {
	root, err := securefs.OpenRoot(tempRoot)
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.RemoveTree(name)
}

func cleanupOtherFallbackDirectories(slot, keep string) {
	locations, err := discoverFallbackLocations(slot)
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
	tempRoot, err := validateRuntimeRoot(tempRoot)
	if err != nil {
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

func clearTmpfsCache(slot string) error {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		return withFallbackLifecycleLock(tempRoot, func() error { return clearCacheAt(slot) })
	}
	return clearCacheAt(slot)
}

func clearCacheAt(slot string) error {
	locations, err := locationsForRead(slot)
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

// clearAllTmpfsCaches removes every vault slot plus any legacy single cache.
func clearAllTmpfsCaches() error {
	var errs []error
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tempRoot := os.TempDir()
		err := withFallbackLifecycleLock(tempRoot, func() error {
			locations, derr := discoverAllFallbackDirectories()
			if derr != nil {
				return derr
			}
			for _, location := range locations {
				cleanupFallbackDirectory(location.root, location.fallback)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	} else {
		runtimeRoot, err := validateRuntimeRoot(os.Getenv("XDG_RUNTIME_DIR"))
		if err != nil {
			errs = append(errs, err)
		} else if root, oerr := securefs.OpenRoot(runtimeRoot); oerr != nil {
			errs = append(errs, oerr)
		} else {
			entries, derr := root.ReadDir("senv")
			if derr != nil && !errors.Is(derr, os.ErrNotExist) {
				errs = append(errs, derr)
			}
			for _, entry := range entries {
				if entry.IsDir || !isSessionCacheName(entry.Name) {
					continue
				}
				if rerr := root.Remove("senv", entry.Name); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
					errs = append(errs, rerr)
				}
			}
			_ = root.Close()
		}
	}
	removeLegacyRuntimeCache()
	removeLegacyPersistentCache()
	return errors.Join(errs...)
}

func isSessionCacheName(name string) bool {
	if name == legacyCacheFileName() {
		return true
	}
	return strings.HasPrefix(name, fmt.Sprintf("session-%d-", os.Getuid()))
}
