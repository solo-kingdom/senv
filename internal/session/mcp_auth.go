package session

import (
	"crypto/sha256"
	"errors"
	"time"
)

const MCPRevocationMessage = "session expired or revoked; restart senv session and MCP server"

var ErrMCPRevoked = errors.New(MCPRevocationMessage)

// MCPAuthorization is an opaque, non-secret fingerprint of the exact session
// that authorized an MCP server at startup. It deliberately retains no salt,
// cached key, password, or manager.
type MCPAuthorization struct {
	sessionID    string
	timeoutType  string
	expiresAt    time.Time
	bootID       string
	dataPathHash string
	saltHash     [sha256.Size]byte
	keyHash      [sha256.Size]byte
}

func fingerprintSecret(value []byte) [sha256.Size]byte {
	return sha256.Sum256(value)
}

func fingerprintText(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}

func authorizationFor(cache *SessionCache, metadataSalt string, key []byte) *MCPAuthorization {
	return &MCPAuthorization{
		sessionID:    cache.SessionID,
		timeoutType:  cache.TimeoutType,
		expiresAt:    cache.ExpiresAt,
		bootID:       cache.BootID,
		dataPathHash: cache.DataPathHash,
		saltHash:     fingerprintText(metadataSalt),
		keyHash:      fingerprintSecret(key),
	}
}

func (a *MCPAuthorization) matches(cache *SessionCache, metadataSalt string, key []byte) bool {
	if a == nil || cache == nil {
		return false
	}
	return a.sessionID == cache.SessionID &&
		a.timeoutType == cache.TimeoutType &&
		a.expiresAt.Equal(cache.ExpiresAt) &&
		a.bootID == cache.BootID &&
		a.dataPathHash == cache.DataPathHash &&
		a.saltHash == fingerprintText(metadataSalt) &&
		a.keyHash == fingerprintSecret(key)
}

// AuthorizeMCPStartup validates the current session once and returns only its
// non-secret fingerprint. The decoded key is zeroed before this method returns.
func (m *Manager) AuthorizeMCPStartup() (*MCPAuthorization, error) {
	key, cache, metadataSalt, err := m.loadValidatedCredential()
	if err != nil {
		return nil, err
	}
	defer ZeroKey(key)
	return authorizationFor(cache, metadataSalt, key), nil
}

// AuthorizeMCPRequest reloads and validates the exact startup session. Every
// failure is deliberately collapsed to one sanitized revocation error.
func (m *Manager) AuthorizeMCPRequest(authorization *MCPAuthorization) ([]byte, error) {
	key, cache, metadataSalt, err := m.loadValidatedCredential()
	if err == nil && !authorization.matches(cache, metadataSalt, key) {
		err = ErrMCPRevoked
	}
	if err != nil {
		ZeroKey(key)
		m.auditMCPRevocation(authorization)
		return nil, ErrMCPRevoked
	}
	return key, nil
}

func (m *Manager) auditMCPRevocation(authorization *MCPAuthorization) {
	if m.auditLogger == nil {
		return
	}
	sessionID := ""
	if authorization != nil {
		sessionID = authorization.sessionID
	}
	_ = m.auditLogger.Log(AuditMCPRevocation, sessionID, false, MCPRevocationMessage)
}

// ZeroKey removes request-scoped key bytes on every handler exit.
func ZeroKey(key []byte) {
	for index := range key {
		key[index] = 0
	}
}
