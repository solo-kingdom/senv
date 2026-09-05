package session

import (
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/storage"
)

// deriveKeyWithIterations is a package-private seam for session boundary tests.
var deriveKeyWithIterations = crypto.DeriveKeyWithIterations

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
		if timeout.Type == TimeoutDuration {
			expiresAt = time.Now().Add(timeout.Value)
		}
		cache := &SessionCache{
			Key:          base64.StdEncoding.EncodeToString(key),
			Salt:         metadata.Salt,
			CreatedAt:    time.Now(),
			ExpiresAt:    expiresAt,
			TimeoutType:  string(timeout.Type),
			BootID:       bootID,
			DataPathHash: hashDataPath(m.dataPath),
			SessionID:    sessionID,
		}
		if err := saveCache(cache); err != nil {
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
// Non-destructive contract: the stale branches (ErrSessionStaleMetadata /
// ErrSessionStaleKey) MUST NOT clear the cache. The cached key may be the only
// remaining credential able to decrypt the user's data files when metadata has
// diverged from them, so callers must keep it until a recovery path is
// confirmed (see cmd-layer diagnosis). Only the genuinely-expired branch
// (ErrSessionExpired) clears, because an expired cache is not a desync recovery
// key and simply forces re-authentication.
func (m *Manager) GetCachedKey() ([]byte, error) {
	key, cache, _, err := m.loadValidatedCredential()
	if err != nil {
		if m.auditLogger != nil && cache != nil {
			_ = m.auditLogger.Log(AuditSessionValidate, cache.SessionID, false, "Session validation failed")
		}
		if errors.Is(err, ErrSessionExpired) {
			_ = clearCache()
		}
		return nil, err
	}
	if m.auditLogger != nil {
		_ = m.auditLogger.Log(AuditSessionValidate, cache.SessionID, true, "Session validated")
	}
	return key, nil
}

// loadValidatedCredential reads one cache snapshot and validates its binding,
// current metadata salt, and cached key. Callers own and must zero a returned
// key. Error paths zero decoded key bytes before returning.
func (m *Manager) loadValidatedCredential() ([]byte, *SessionCache, string, error) {
	cache, err := loadCacheForDataPath(m.dataPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to load cache: %w", err)
	}
	if cache == nil {
		return nil, nil, "", ErrNoSession
	}
	valid, err := m.isCacheValid(cache)
	if err != nil || !valid {
		return nil, cache, "", ErrSessionExpired
	}
	key, err := base64.StdEncoding.DecodeString(cache.Key)
	if err != nil {
		return nil, cache, "", fmt.Errorf("failed to decode key: %w", err)
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

// isCacheValid checks if the cache is still valid
func (m *Manager) isCacheValid(cache *SessionCache) (bool, error) {
	if cache.DataPathHash != hashDataPath(m.dataPath) {
		return false, fmt.Errorf("data path mismatch")
	}
	currentBootID, err := systemBootID()
	if err != nil {
		return false, err
	}
	if cache.BootID == "" || cache.BootID != currentBootID {
		return false, nil
	}

	switch cache.TimeoutType {
	case string(TimeoutRestart):
		return true, nil
	case string(TimeoutDuration):
		return time.Now().Before(cache.ExpiresAt), nil
	default:
		return false, fmt.Errorf("unknown timeout type: %s", cache.TimeoutType)
	}
}

// ClearSession removes the session cache
func (m *Manager) ClearSession() error {
	cache, _ := loadCache()
	if cache != nil && m.auditLogger != nil {
		m.auditLogger.Log(AuditSessionClear, cache.SessionID, true, "Session cleared by user")
	}

	return clearCache()
}

// LoadCache loads the session cache (public method for status command)
func (m *Manager) LoadCache() (*SessionCache, error) {
	return loadCacheForDataPath(m.dataPath)
}

// IsCacheValid checks if the cache is valid (public method for status command)
func (m *Manager) IsCacheValid(cache *SessionCache) (bool, error) {
	return m.isCacheValid(cache)
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
