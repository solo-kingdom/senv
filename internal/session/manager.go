package session

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/storage"
)

// deriveKeyWithIterations is a package-private seam for session boundary tests.
var deriveKeyWithIterations = crypto.DeriveKeyWithIterations

// DefaultMaxLifetime is the default absolute ceiling for sliding renewal.
const DefaultMaxLifetime = 24 * time.Hour

// timeNow is the clock seam for session lifetime tests; production never replaces it.
var timeNow = time.Now

// Manager handles session management
type Manager struct {
	configPath  string
	dataPath    string
	auditLogger *AuditLogger
}

// NewManager creates a new session manager
func NewManager(configPath string, dataPath string) *Manager {
	auditLogger, _ := NewAuditLogger(configPath)
	return &Manager{
		configPath:  configPath,
		dataPath:    dataPath,
		auditLogger: auditLogger,
	}
}

// slot returns this manager's vault slot identity.
func (m *Manager) slot() string { return vaultSlotFor(m.dataPath) }

// maxLifetime resolves the configured renewal ceiling, falling back to
// DefaultMaxLifetime when unset or unparseable.
func (m *Manager) maxLifetime() time.Duration {
	settings, err := storage.NewManager(m.configPath, m.dataPath).LoadSettings()
	if err != nil {
		return DefaultMaxLifetime
	}
	raw := strings.TrimSpace(settings.Session.MaxLifetime)
	if raw == "" {
		return DefaultMaxLifetime
	}
	parsed, err := ParseTimeout(raw)
	if err != nil || parsed == nil || parsed.Type != TimeoutDuration || parsed.Value <= 0 {
		return DefaultMaxLifetime
	}
	return parsed.Value
}

// AutoStartEnabled reports whether password-based commands may rebuild a
// persistent session (opt-in, default off).
func (m *Manager) AutoStartEnabled() bool {
	settings, err := storage.NewManager(m.configPath, m.dataPath).LoadSettings()
	if err != nil {
		return false
	}
	return settings.Session.AutoStart
}

// StartSession creates a new session with the given password and timeout.
// Authentication, key derivation, verification, and cache persistence share a
// vault mutation lease so a concurrent rekey cannot leave a stale cache behind.
func (m *Manager) StartSession(password string, timeout *SessionTimeout) error {
	storageManager := storage.NewManager(m.configPath, m.dataPath)
	var sessionID string
	err := storageManager.WithVaultMutation(func(locked *storage.Manager) error {
		metadata, err := locked.LoadMetadata()
		if err != nil {
			return fmt.Errorf("failed to load metadata: %w", err)
		}
		salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
		if err != nil {
			return fmt.Errorf("failed to decode salt: %w", err)
		}
		iterations, err := metadata.ValidatedKDFIterations()
		if err != nil {
			return err
		}
		key := deriveKeyWithIterations(password, salt, iterations)
		defer ZeroKey(key)

		passwordHash, err := crypto.Decrypt(key, metadata.PasswordKey)
		if err != nil || crypto.HashPassword(password) != string(passwordHash) {
			return errInvalidSessionPassword
		}
		valid, err := locked.VerifyKey(key)
		if err != nil {
			return fmt.Errorf("failed to verify derived key: %w", err)
		}
		if !valid {
			return errInvalidSessionPassword
		}

		sessionID, err = generateSessionID()
		if err != nil {
			return fmt.Errorf("failed to start session: %w", err)
		}
		bootID, err := systemBootID()
		if err != nil {
			return fmt.Errorf("failed to get boot ID: %w", err)
		}
		expiresAt := time.Time{}
		timeoutSeconds := int64(0)
		if timeout.Type == TimeoutDuration {
			expiresAt = timeNow().Add(timeout.Value)
			timeoutSeconds = int64(timeout.Value.Seconds())
		}
		cache := &SessionCache{
			Key:            base64.StdEncoding.EncodeToString(key),
			Salt:           metadata.Salt,
			CreatedAt:      timeNow(),
			ExpiresAt:      expiresAt,
			TimeoutType:    string(timeout.Type),
			BootID:         bootID,
			DataPathHash:   m.slot(),
			SessionID:      sessionID,
			TimeoutSeconds: timeoutSeconds,
		}
		if err := saveCache(m.slot(), cache); err != nil {
			return fmt.Errorf("failed to save session cache: %w", err)
		}
		return nil
	})
	if errors.Is(err, errInvalidSessionPassword) {
		if m.auditLogger != nil {
			m.auditLogger.Log(AuditAuthFailure, "", false, "Invalid password")
		}
		return fmt.Errorf("invalid password")
	}
	if err != nil {
		return err
	}
	if m.auditLogger != nil {
		m.auditLogger.LogWithDetails(AuditSessionStart, sessionID, true, fmt.Sprintf("Session started with timeout: %s", timeout.String()), string(timeout.Type), timeout.String())
	}
	return nil
}

var errInvalidSessionPassword = errors.New("invalid session password")

// GetCachedKey retrieves the cached key if the session is still valid.
//
// Destructive contract: only a genuinely timed-out cache (ErrSessionExpired,
// i.e. ReasonExpired) is cleared. Invalidated caches (ErrSessionInvalidated:
// system rebooted for a "restart" session, or the cache belongs to another
// vault) are preserved, because a vault-mismatched cache may be the only
// recovery key for that other vault. Unverifiable caches (environmental
// failures, corrupt payloads, duplicate slots) and stale caches
// (ErrSessionStaleMetadata / ErrSessionStaleKey) are preserved for the same
// reason. See CONTEXT.md "自动清理" and ADR-0017.
func (m *Manager) GetCachedKey() ([]byte, error) {
	key, cache, _, err := m.loadValidatedCredential()
	if err != nil {
		m.auditValidationFailure(cache, err)
		if errors.Is(err, ErrSessionExpired) {
			_ = clearCache(m.slot())
		}
		return nil, err
	}
	if m.auditLogger != nil {
		_ = m.auditLogger.Log(AuditSessionValidate, cache.SessionID, true, "Session validated")
	}
	m.renewOnUse(cache)
	return key, nil
}

// renewOnUse slides a duration session's expiry after a business command reused
// it. Best-effort: a failed renewal must not break a command that already
// authenticated, and the absolute ceiling still applies.
func (m *Manager) renewOnUse(cache *SessionCache) {
	if cache.TimeoutType != string(TimeoutDuration) || cache.TimeoutSeconds <= 0 {
		return
	}
	now := timeNow()
	timeout := time.Duration(cache.TimeoutSeconds) * time.Second
	expiry := renewalExpiry(cache.CreatedAt, timeout, m.maxLifetime(), now)
	if !expiry.After(cache.ExpiresAt) {
		return
	}
	updated := *cache
	updated.ExpiresAt = expiry
	_ = saveCacheQuiet(m.slot(), &updated)
}

// renewalExpiry slides expiry to now+timeout without ever crossing the
// session's absolute ceiling. An explicit timeout larger than the ceiling is
// never shortened by the default ceiling.
func renewalExpiry(createdAt time.Time, timeout, maxLifetime time.Duration, now time.Time) time.Time {
	ceiling := maxLifetime
	if timeout > ceiling {
		ceiling = timeout
	}
	limit := createdAt.Add(ceiling)
	expiry := now.Add(timeout)
	if expiry.After(limit) {
		expiry = limit
	}
	return expiry
}

func (m *Manager) auditValidationFailure(cache *SessionCache, err error) {
	if m.auditLogger == nil {
		return
	}
	sessionID := ""
	if cache != nil {
		sessionID = cache.SessionID
	}
	switch {
	case errors.Is(err, ErrSessionExpired):
		_ = m.auditLogger.Log(AuditSessionExpire, sessionID, false, "Session expired")
	case errors.Is(err, ErrSessionInvalidated):
		_ = m.auditLogger.Log(AuditSessionInvalidated, sessionID, false, "Session invalidated: "+err.Error())
	case errors.Is(err, ErrSessionUnverifiable):
		_ = m.auditLogger.Log(AuditSessionUnverifiable, sessionID, false, "Session unverifiable: "+err.Error())
	default:
		if cache != nil {
			_ = m.auditLogger.Log(AuditSessionValidate, sessionID, false, "Session validation failed")
		}
	}
}

// loadValidatedCredential reads one cache snapshot and validates its binding,
// current metadata salt, and cached key. Callers own and must zero a returned
// key. Error paths zero decoded key bytes before returning.
func (m *Manager) loadValidatedCredential() ([]byte, *SessionCache, string, error) {
	cache, err := loadCacheForDataPath(m.dataPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: %w", ErrSessionUnverifiable, err)
	}
	if cache == nil {
		return nil, nil, "", ErrNoSession
	}
	reason, err := m.cacheValidity(cache)
	if err != nil {
		return nil, cache, "", fmt.Errorf("%w: %w", ErrSessionUnverifiable, err)
	}
	switch reason {
	case ReasonNone:
	case ReasonExpired:
		return nil, cache, "", ErrSessionExpired
	case ReasonRestarted:
		return nil, cache, "", fmt.Errorf("%w: %s", ErrSessionInvalidated, reason)
	case ReasonVaultChanged:
		return nil, cache, "", fmt.Errorf("%w: %w", ErrSessionInvalidated, ErrSessionVaultChanged)
	default:
		return nil, cache, "", fmt.Errorf("%w: %s", ErrSessionUnverifiable, reason)
	}
	key, err := base64.StdEncoding.DecodeString(cache.Key)
	if err != nil {
		return nil, cache, "", fmt.Errorf("%w: failed to decode key: %w", ErrSessionUnverifiable, err)
	}
	storageManager := storage.NewManager(m.configPath, m.dataPath)
	metadata, err := storageManager.LoadMetadata()
	if err != nil {
		ZeroKey(key)
		return nil, cache, "", fmt.Errorf("failed to load metadata: %w", err)
	}
	if cache.Salt != metadata.Salt {
		ZeroKey(key)
		return nil, cache, metadata.Salt, ErrSessionStaleMetadata
	}
	keyValid, err := storageManager.VerifyKey(key)
	if err != nil {
		ZeroKey(key)
		return nil, cache, metadata.Salt, err
	}
	if !keyValid {
		ZeroKey(key)
		return nil, cache, metadata.Salt, ErrSessionStaleKey
	}
	return key, cache, metadata.Salt, nil
}

// PeekCachedKey returns the raw cached key and cache without any validation or
// clearing. It is intended for diagnosis: when GetCachedKey reports a stale
// session, the caller can use PeekCachedKey to probe whether the cached key
// still decrypts the data files (recovery possible) or not. It does not touch
// the cache on disk.
func (m *Manager) PeekCachedKey() ([]byte, *SessionCache, error) {
	cache, err := loadCacheForDataPath(m.dataPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load cache: %w", err)
	}
	if cache == nil {
		return nil, nil, ErrNoSession
	}
	key, err := base64.StdEncoding.DecodeString(cache.Key)
	if err != nil {
		return nil, cache, fmt.Errorf("failed to decode key: %w", err)
	}
	return key, cache, nil
}

// cacheValidity classifies why a cache can or cannot be reused. A returned
// error means the reason itself could not be determined (unverifiable).
func (m *Manager) cacheValidity(cache *SessionCache) (InvalidReason, error) {
	if cache.DataPathHash != m.slot() {
		return ReasonVaultChanged, nil
	}
	switch cache.TimeoutType {
	case string(TimeoutRestart), "never":
		currentBootID, err := systemBootID()
		if err != nil {
			return ReasonUnreadable, err
		}
		if cache.BootID == "" || cache.BootID != currentBootID {
			return ReasonRestarted, nil
		}
		return ReasonNone, nil
	case string(TimeoutDuration):
		if timeNow().Before(cache.ExpiresAt) {
			return ReasonNone, nil
		}
		return ReasonExpired, nil
	default:
		return ReasonUnknownType, nil
	}
}

// IsCacheValid checks if the cache is valid (public method for status command).
func (m *Manager) IsCacheValid(cache *SessionCache) (bool, error) {
	reason, err := m.cacheValidity(cache)
	if err != nil {
		return false, err
	}
	return reason == ReasonNone, nil
}

// CacheStatus is the read-only status of the current vault's session.
type CacheStatus struct {
	State     SessionState
	Reason    InvalidReason
	Detail    string
	Cache     *SessionCache
	Retained  bool
	ExpiresAt time.Time
}

// DescribeCache reports the current session state without clearing anything.
func (m *Manager) DescribeCache() CacheStatus {
	cache, err := m.LoadCache()
	if err != nil {
		return CacheStatus{
			State:    StateUnverifiable,
			Reason:   ReasonUnreadable,
			Detail:   err.Error(),
			Retained: true,
		}
	}
	if cache == nil {
		return CacheStatus{State: StateNoSession}
	}
	reason, verr := m.cacheValidity(cache)
	switch {
	case verr != nil:
		return CacheStatus{State: StateUnverifiable, Reason: ReasonUnreadable, Detail: verr.Error(), Cache: cache, Retained: true}
	case reason == ReasonNone:
		return CacheStatus{State: StateActive, Cache: cache, Retained: true, ExpiresAt: cache.ExpiresAt}
	case reason == ReasonExpired:
		return CacheStatus{State: StateExpired, Reason: reason, Cache: cache, Retained: true, ExpiresAt: cache.ExpiresAt}
	case reason == ReasonRestarted, reason == ReasonVaultChanged:
		return CacheStatus{State: StateInvalidated, Reason: reason, Cache: cache, Retained: true}
	default:
		return CacheStatus{State: StateUnverifiable, Reason: reason, Cache: cache, Retained: true}
	}
}

// HasValidSession reports whether the current vault has a reusable session.
func (m *Manager) HasValidSession() bool {
	return m.DescribeCache().State == StateActive
}

// RenewSession extends an already-validated session without a password. An
// explicit timeout resets the sliding window; the absolute ceiling and
// CreatedAt are preserved so renewal cannot outlive the vault's session cap.
func (m *Manager) RenewSession(timeout *SessionTimeout) error {
	if timeout == nil {
		return fmt.Errorf("session cache is disabled in configuration")
	}
	key, cache, _, err := m.loadValidatedCredential()
	if err != nil {
		return err
	}
	ZeroKey(key)

	updated := *cache
	switch timeout.Type {
	case TimeoutRestart:
		bootID, err := systemBootID()
		if err != nil {
			return fmt.Errorf("failed to get boot ID: %w", err)
		}
		updated.TimeoutType = string(TimeoutRestart)
		updated.ExpiresAt = time.Time{}
		updated.BootID = bootID
		updated.TimeoutSeconds = 0
	case TimeoutDuration:
		updated.TimeoutType = string(TimeoutDuration)
		updated.ExpiresAt = renewalExpiry(cache.CreatedAt, timeout.Value, m.maxLifetime(), timeNow())
		updated.TimeoutSeconds = int64(timeout.Value.Seconds())
	default:
		return fmt.Errorf("unknown timeout type: %s", timeout.Type)
	}
	if err := saveCacheQuiet(m.slot(), &updated); err != nil {
		return fmt.Errorf("failed to save session cache: %w", err)
	}
	if m.auditLogger != nil {
		_ = m.auditLogger.LogWithDetails(AuditSessionStart, updated.SessionID, true,
			fmt.Sprintf("Session refreshed with timeout: %s", timeout.String()), string(timeout.Type), timeout.String())
	}
	return nil
}

// ClearSession removes the current vault's session cache.
func (m *Manager) ClearSession() error {
	cache, _ := m.LoadCache()
	if cache != nil && m.auditLogger != nil {
		m.auditLogger.Log(AuditSessionClear, cache.SessionID, true, "Session cleared by user")
	}
	return clearCache(m.slot())
}

// ClearAllSessions removes every vault slot plus legacy single-cache residue.
func (m *Manager) ClearAllSessions() error {
	if m.auditLogger != nil {
		m.auditLogger.Log(AuditSessionClear, "", true, "All sessions cleared by user")
	}
	return clearAllCaches()
}

// LoadCache loads the session cache (public method for status command)
func (m *Manager) LoadCache() (*SessionCache, error) {
	return loadCacheForDataPath(m.dataPath)
}

// GetAuditLogger returns the audit logger
func (m *Manager) GetAuditLogger() *AuditLogger {
	return m.auditLogger
}

// Close releases resources held by the session manager.
func (m *Manager) Close() error {
	if m == nil || m.auditLogger == nil {
		return nil
	}
	return m.auditLogger.Close()
}
