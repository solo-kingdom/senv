package cmd

import (
	"errors"
	"fmt"

	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// authResult holds resolved credentials for a command invocation. Exactly one
// auth method is populated: key (session reuse) or password (temporary auth).
type authResult struct {
	storage  *storage.Manager
	key      []byte // non-nil when authenticated via a valid session cache
	password string // non-empty when authenticated via password prompt
}

func (a *authResult) hasKey() bool { return a.key != nil }

// resolveAuth authenticates the user for a command invocation.
//
// It reuses a valid session when available. When the session is stale, it
// diagnoses the cause instead of misreporting it as a wrong-password failure:
//
//   - If the cached key still decrypts data files but not metadata.PasswordKey,
//     the project is genuinely desynced (metadata replaced, e.g. by git pull).
//     resolveAuth returns an error wrapping storage.ErrDataDesync and preserves
//     the cache as a recovery key — it does NOT prompt for a password, because
//     the password would be verified against the very metadata that is broken.
//   - If the cached key decrypts nothing useful, the cache is genuinely stale
//     and is cleared before falling back to the password prompt.
//
// On the password path, a lingering stale cache is cleared only after
// VerifyPassword succeeds (proving metadata is consistent with the password).
// The password path never writes a session cache.
func resolveAuth(configPath, dataPath string, prompt passwordPrompter) (*authResult, error) {
	store := storage.NewManager(configPath, dataPath)
	if !store.IsInitialized() {
		return nil, errNotInitialized
	}
	sm := session.NewManager(configPath, dataPath)

	// 1. Try session reuse.
	key, err := sm.GetCachedKey()
	if err == nil {
		return &authResult{storage: store, key: key}, nil
	}

	// 2. Diagnose stale sessions before prompting for a password.
	stale := errors.Is(err, session.ErrSessionStaleMetadata) || errors.Is(err, session.ErrSessionStaleKey)
	if stale {
		if diag := diagnoseStaleSession(sm, store); diag != nil {
			return nil, diag
		}
		// diag == nil => cache is genuinely useless; it has already been cleared.
	}

	// 3. Fall back to password (temporary auth; does not write a session).
	password, perr := prompt("Senv - Enter password: ")
	if perr != nil {
		return nil, fmt.Errorf("failed to read password: %w", perr)
	}
	valid, verr := store.VerifyPassword(password)
	if verr != nil {
		return nil, fmt.Errorf("failed to verify password: %w", verr)
	}
	if !valid {
		return nil, errInvalidPassword
	}

	// 4. Password verified => metadata is consistent with it. A lingering stale
	// cache is now proven useless; clear it so subsequent commands reuse the
	// password path cleanly instead of repeatedly hitting the stale key.
	if stale {
		_ = sm.ClearSession()
	}
	return &authResult{storage: store, password: password}, nil
}

// diagnoseStaleSession probes the cached key against the data files.
//
// Returns a non-nil error (wrapping storage.ErrDataDesync) when the key still
// decrypts data but not metadata — i.e. a real desync that must be reported,
// not papered over with a password prompt.
//
// Returns nil when the cached key decrypts nothing useful; in that case the
// stale cache has been cleared and the caller should fall back to a password.
//
// Returns nil also when diagnosis cannot be performed (e.g. cache unreadable),
// in which case the caller falls back to a password without clearing.
func diagnoseStaleSession(sm *session.Manager, store *storage.Manager) error {
	peekedKey, _, perr := sm.PeekCachedKey()
	if perr != nil {
		return nil
	}
	report, rerr := store.CheckConsistency(peekedKey)
	if rerr != nil {
		return nil
	}
	dataOK := report.EnvFiles.OK + report.TextFiles.OK + report.ConfigFiles.OK
	if dataOK > 0 && !report.MetadataKeyOK {
		// Real desync: preserve the cache as recovery key; do not prompt.
		return fmt.Errorf("%w: cached key still decrypts %d data file(s) but not metadata. "+
			"metadata.json appears replaced (e.g. via git pull / re-init). "+
			"Run `senv doctor` for details; the session key is preserved as a recovery key",
			storage.ErrDataDesync, dataOK)
	}
	if dataOK == 0 && !report.MetadataKeyOK {
		// Cached key decrypts nothing => genuinely useless; safe to clear.
		_ = sm.ClearSession()
	}
	return nil
}
