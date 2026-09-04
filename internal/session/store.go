package session

import (
	"errors"
	"runtime"
)

// SessionStore persists the session cache in a platform-verified secure store.
// Load returns (nil, nil) when no cache exists so callers can distinguish
// "no session" from store failures.
type SessionStore interface {
	Save(cache *SessionCache) error
	Load() (*SessionCache, error)
	Clear() error
}

// tmpfsStore keeps the historical memory-backed filesystem implementation:
// XDG runtime with random 0700 fallback directories, flock serialization, and
// securefs no-follow anchoring.
type tmpfsStore struct{}

func (tmpfsStore) Save(cache *SessionCache) error { return saveTmpfsCache(cache) }
func (tmpfsStore) Load() (*SessionCache, error)   { return loadTmpfsCache() }
func (tmpfsStore) Clear() error                   { return clearTmpfsCache() }

// defaultSessionStore selects the platform-verified secure store: the macOS
// login keychain on darwin, and the hardened memory-backed filesystem
// implementation elsewhere.
func defaultSessionStore() SessionStore {
	if runtime.GOOS == "darwin" {
		return keychainStore{}
	}
	return tmpfsStore{}
}

// errMultipleSessionCaches protects the single-session invariant when both the
// platform store and the escape hatch hold valid-looking caches.
var errMultipleSessionCaches = errors.New("multiple session caches found; clear the session")

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

// activeSessionStore is the package-level store seam; tests may inject fakes.
var activeSessionStore SessionStore = defaultSessionStore()

func saveCache(cache *SessionCache) error {
	var err error
	if insecureCacheEnabled {
		err = (diskCacheStore{}).Save(cache)
	} else {
		err = activeSessionStore.Save(cache)
	}
	if err != nil {
		return err
	}
	// Every successful write removes caches left by pre-hardening releases.
	removeLegacyRuntimeCache()
	removeLegacyPersistentCache()
	return nil
}

func loadCache() (*SessionCache, error) {
	primary, primaryErr := activeSessionStore.Load()
	hatch, err := diskCacheStore{}.Load()
	if err != nil {
		return nil, err
	}
	if primaryErr != nil {
		// A locked or unavailable platform store must not strand an existing
		// escape-hatch session (headless macOS/CI), but without a hatch cache
		// the actionable platform error stays visible.
		if hatch != nil {
			return hatch, nil
		}
		return nil, primaryErr
	}
	if primary != nil && hatch != nil {
		return nil, errMultipleSessionCaches
	}
	if primary != nil {
		return primary, nil
	}
	return hatch, nil
}

func clearCache() error {
	return errors.Join(activeSessionStore.Clear(), diskCacheStore{}.Clear())
}
