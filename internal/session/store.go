package session

import (
	"errors"
	"runtime"
)

// SessionStore persists one vault's session cache in a platform-verified
// secure store. Load returns (nil, nil) when the slot holds no cache so callers
// can distinguish "no session" from store failures.
type SessionStore interface {
	Save(slot string, cache *SessionCache) error
	Load(slot string) (*SessionCache, error)
	Clear(slot string) error
	// ClearAll removes every vault slot plus legacy single-cache residue.
	ClearAll() error
	// LoadLegacy reads a pre-slot (single-cache) entry left by older releases.
	LoadLegacy() (*SessionCache, error)
	// ClearLegacy removes that entry after adoption.
	ClearLegacy() error
}

// tmpfsStore keeps the historical memory-backed filesystem implementation:
// XDG runtime with random 0700 fallback directories, flock serialization, and
// securefs no-follow anchoring. Files are named per vault slot.
type tmpfsStore struct{}

func (tmpfsStore) Save(slot string, cache *SessionCache) error { return saveTmpfsCache(slot, cache) }
func (tmpfsStore) Load(slot string) (*SessionCache, error)     { return loadTmpfsCache(slot) }
func (tmpfsStore) Clear(slot string) error                     { return clearTmpfsCache(slot) }
func (tmpfsStore) ClearAll() error                             { return clearAllTmpfsCaches() }
func (tmpfsStore) LoadLegacy() (*SessionCache, error)          { return loadLegacyTmpfsCache() }
func (tmpfsStore) ClearLegacy() error                          { return clearLegacyTmpfsCache() }

// defaultSessionStoreFor selects the platform-verified secure store: the macOS
// login keychain on darwin, and the hardened memory-backed filesystem
// implementation elsewhere.
func defaultSessionStoreFor(slot string) SessionStore {
	if runtime.GOOS == "darwin" {
		return keychainStore{slot: slot}
	}
	return tmpfsStore{}
}

// errMultipleSessionCaches protects the single-session invariant when both the
// platform store and the escape hatch hold valid-looking caches.
var errMultipleSessionCaches = errors.New("multiple session caches found; run: senv session clear --all")

// insecureCacheEnabled records the explicit --insecure-cache opt-in from the
// CLI. It only redirects writes; reads always inspect both stores.
var insecureCacheEnabled bool

// InsecureCacheWarning is printed to stderr before the escape hatch is used.
const InsecureCacheWarning = "WARNING: --insecure-cache will store the derived session key unencrypted on disk (0600). " +
	"Any process running as your user, backups, and sync tools may read it. " +
	"Use this escape hatch only in headless macOS or CI environments without a secure store."

// EnableInsecureCache redirects session cache writes to the explicit opt-in
// disk escape hatch. Reads always consider both stores.
func EnableInsecureCache() {
	insecureCacheEnabled = true
}

// activeSessionStoreFor is the package-level store seam; tests may inject fakes.
var activeSessionStoreFor func(slot string) SessionStore = defaultSessionStoreFor

func saveCache(slot string, cache *SessionCache) error {
	var err error
	if insecureCacheEnabled {
		err = (diskCacheStore{}).Save(slot, cache)
	} else {
		err = activeSessionStoreFor(slot).Save(slot, cache)
	}
	if err != nil {
		return err
	}
	// Every successful write removes caches left by pre-hardening releases.
	removeLegacyRuntimeCache()
	removeLegacyPersistentCache()
	return nil
}

func clearCache(slot string) error {
	return errors.Join(activeSessionStoreFor(slot).Clear(slot), (diskCacheStore{}).Clear(slot))
}

// clearAllCaches removes every vault slot plus legacy single-cache residue.
func clearAllCaches() error {
	return errors.Join(activeSessionStoreFor("").ClearAll(), (diskCacheStore{}).ClearAll())
}
