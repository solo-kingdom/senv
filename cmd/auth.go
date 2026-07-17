package cmd

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"golang.org/x/term"
)

// ErrNeedSession is returned when authentication would require a password
// prompt but the invocation is non-interactive (or env export stdout is
// captured). The error text guides the user to start a session.
var ErrNeedSession = errors.New("no active session; run: senv session start")

// authResult holds resolved credentials for a command invocation. Exactly one
// auth method is populated: key (session reuse) or password (temporary auth).
type authResult struct {
	storage  *storage.Manager
	key      []byte // non-nil when authenticated via a valid session cache
	password string // non-empty when authenticated via password prompt
}

func (a *authResult) hasKey() bool { return a.key != nil }

// authOptions controls prompt eligibility beyond the default stdin-TTY check.
type authOptions struct {
	// requireStdoutTTY treats non-TTY stdout as non-interactive. Used by
	// `env export` so `eval $(senv env export)` does not prompt for a password.
	requireStdoutTTY bool
}

// Process-local memo of successful auth results, keyed by configPath+dataPath.
// Failures are never stored. Cleared via clearAuthMemo (tests) or process exit.
var (
	authMemoMu sync.Mutex
	authMemo   map[string]*authResult

	// Overridable in tests.
	stdinIsTerminal  = defaultStdinIsTerminal
	stdoutIsTerminal = defaultStdoutIsTerminal

	// authPrompt is used by get*Manager entry points. Tests may replace it.
	authPrompt passwordPrompter = promptPassword

	// activeAuthOpts is consulted by resolveAuth for the current call. Commands
	// that need special prompt rules (e.g. env export) set it for the duration
	// of their RunE and restore afterward.
	activeAuthOpts authOptions
)

func defaultStdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func defaultStdoutIsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func authMemoKey(configPath, dataPath string) string {
	return configPath + "\x00" + dataPath
}

func lookupAuthMemo(configPath, dataPath string) *authResult {
	authMemoMu.Lock()
	defer authMemoMu.Unlock()
	if authMemo == nil {
		return nil
	}
	return authMemo[authMemoKey(configPath, dataPath)]
}

func storeAuthMemo(configPath, dataPath string, auth *authResult) {
	authMemoMu.Lock()
	defer authMemoMu.Unlock()
	if authMemo == nil {
		authMemo = make(map[string]*authResult)
	}
	authMemo[authMemoKey(configPath, dataPath)] = auth
}

// clearAuthMemo clears the process-local auth memo. Intended for tests.
func clearAuthMemo() {
	authMemoMu.Lock()
	defer authMemoMu.Unlock()
	authMemo = nil
}

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
//
// Successful results are memoized for the process lifetime so getEnvManager /
// getTextManager / resolveValue do not re-prompt within the same invocation.
func resolveAuth(configPath, dataPath string, prompt passwordPrompter) (*authResult, error) {
	if cached := lookupAuthMemo(configPath, dataPath); cached != nil {
		return cached, nil
	}

	store := storage.NewManager(configPath, dataPath)
	if !store.IsInitialized() {
		return nil, errNotInitialized
	}
	sm := session.NewManager(configPath, dataPath)

	// 1. Try session reuse.
	key, err := sm.GetCachedKey()
	if err == nil {
		auth := &authResult{storage: store, key: key}
		storeAuthMemo(configPath, dataPath, auth)
		return auth, nil
	}

	// 2. Diagnose stale sessions before prompting for a password.
	stale := errors.Is(err, session.ErrSessionStaleMetadata) || errors.Is(err, session.ErrSessionStaleKey)
	if stale {
		if diag := diagnoseStaleSession(sm, store); diag != nil {
			return nil, diag
		}
		// diag == nil => cache is genuinely useless; it has already been cleared.
	}

	// 3. Non-interactive / captured-stdout: refuse to prompt.
	if !stdinIsTerminal() || (activeAuthOpts.requireStdoutTTY && !stdoutIsTerminal()) {
		return nil, ErrNeedSession
	}

	// 4. Fall back to password (temporary auth; does not write a session).
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

	// 5. Password verified => metadata is consistent with it. A lingering stale
	// cache is now proven useless; clear it so subsequent commands reuse the
	// password path cleanly instead of repeatedly hitting the stale key.
	if stale {
		_ = sm.ClearSession()
	}
	auth := &authResult{storage: store, password: password}
	storeAuthMemo(configPath, dataPath, auth)
	return auth, nil
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
