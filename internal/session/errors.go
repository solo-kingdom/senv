package session

import "errors"

// Sentinel errors returned by GetCachedKey. Callers (especially the cmd layer)
// use errors.Is to distinguish a recoverable "just re-authenticate" state from a
// potentially dangerous data-desync state, and to avoid destroying a cache that
// may be the only remaining key able to decrypt the user's data.
var (
	// ErrNoSession is returned when no session cache file exists at all. The
	// caller should fall back to prompting for a password.
	ErrNoSession = errors.New("no active session")

	// ErrSessionExpired is returned when a cache exists but its timeout has
	// elapsed (duration expired or system rebooted for "restart" type). The
	// cache is genuinely unusable; clearing it is safe.
	ErrSessionExpired = errors.New("session expired")

	// ErrSessionStaleMetadata is returned when the cache's salt no longer
	// matches metadata.json, which typically means metadata was replaced
	// (e.g. git pull brought another machine's re-initialized metadata).
	// The cached key may still decrypt the data files and therefore MUST NOT
	// be discarded automatically.
	ErrSessionStaleMetadata = errors.New("session stale: metadata changed")

	// ErrSessionStaleKey is returned when the salt still matches but the cached
	// key can no longer decrypt metadata.PasswordKey. This indicates an
	// internal inconsistency that warrants diagnosis rather than silent clearing.
	ErrSessionStaleKey = errors.New("session stale: key invalid")
)
