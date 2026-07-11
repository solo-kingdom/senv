package session

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/storage"
)

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

// StartSession creates a new session with the given password and timeout
func (m *Manager) StartSession(password string, timeout *SessionTimeout) error {
	// Verify password
	storageManager := storage.NewManager(m.configPath, m.dataPath)
	valid, err := storageManager.VerifyPassword(password)
	if err != nil {
		return fmt.Errorf("failed to verify password: %w", err)
	}
	if !valid {
		if m.auditLogger != nil {
			m.auditLogger.Log(AuditAuthFailure, "", false, "Invalid password")
		}
		return fmt.Errorf("invalid password")
	}

	// Get metadata for salt
	metadata, err := storageManager.LoadMetadata()
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	// Derive key
	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		return fmt.Errorf("failed to decode salt: %w", err)
	}
	key := crypto.DeriveKey(password, salt)

	// Create session cache
	sessionID := generateSessionID()
	bootID := ""
	expiresAt := time.Time{} // Zero time for never/restart

	switch timeout.Type {
	case TimeoutRestart:
		bootID, err = GetSystemBootID()
		if err != nil {
			return fmt.Errorf("failed to get boot ID: %w", err)
		}
	case TimeoutDuration:
		expiresAt = time.Now().Add(timeout.Value)
	case TimeoutNever:
		// expiresAt remains zero
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

	// Save cache
	if err := saveCache(cache); err != nil {
		return fmt.Errorf("failed to save session cache: %w", err)
	}

	// Log audit event
	if m.auditLogger != nil {
		m.auditLogger.LogWithDetails(
			AuditSessionStart,
			sessionID,
			true,
			fmt.Sprintf("Session started with timeout: %s", timeout.String()),
			string(timeout.Type),
			timeout.String(),
		)
	}

	return nil
}

// GetCachedKey retrieves the cached key if the session is still valid
func (m *Manager) GetCachedKey() ([]byte, error) {
	cache, err := loadCacheForDataPath(m.dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache: %w", err)
	}
	if cache == nil {
		return nil, fmt.Errorf("no active session")
	}

	// Validate cache
	valid, err := m.isCacheValid(cache)
	if err != nil || !valid {
		if m.auditLogger != nil {
			reason := "Session expired"
			if err != nil {
				reason = fmt.Sprintf("Session validation failed: %v", err)
			}
			m.auditLogger.Log(AuditSessionValidate, cache.SessionID, false, reason)
		}
		_ = clearCache()
		return nil, fmt.Errorf("session expired or invalid")
	}

	// Decode key
	key, err := base64.StdEncoding.DecodeString(cache.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}

	storageManager := storage.NewManager(m.configPath, m.dataPath)
	metadata, err := storageManager.LoadMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Reject stale sessions when metadata changed (e.g. git pull, re-init).
	if cache.Salt != metadata.Salt {
		if m.auditLogger != nil {
			m.auditLogger.Log(AuditSessionValidate, cache.SessionID, false, "Session stale: salt mismatch")
		}
		_ = clearCache()
		return nil, fmt.Errorf("session stale")
	}

	keyValid, err := storageManager.VerifyKey(key)
	if err != nil {
		return nil, err
	}
	if !keyValid {
		if m.auditLogger != nil {
			m.auditLogger.Log(AuditSessionValidate, cache.SessionID, false, "Session stale: key invalid")
		}
		_ = clearCache()
		return nil, fmt.Errorf("session stale")
	}

	// Log successful validation
	if m.auditLogger != nil {
		m.auditLogger.Log(AuditSessionValidate, cache.SessionID, true, "Session validated")
	}

	return key, nil
}

// isCacheValid checks if the cache is still valid
func (m *Manager) isCacheValid(cache *SessionCache) (bool, error) {
	// Verify data path
	if cache.DataPathHash != hashDataPath(m.dataPath) {
		return false, fmt.Errorf("data path mismatch")
	}

	switch cache.TimeoutType {
	case string(TimeoutNever):
		return true, nil

	case string(TimeoutRestart):
		currentBootID, err := GetSystemBootID()
		if err != nil {
			return false, err
		}
		return cache.BootID == currentBootID, nil

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
