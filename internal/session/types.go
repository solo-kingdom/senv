package session

import "time"

// TimeoutType defines the type of session timeout
type TimeoutType string

const (
	TimeoutDuration TimeoutType = "duration" // Fixed duration (e.g., 8h, 1d)
	TimeoutRestart  TimeoutType = "restart"  // Until system restart
	TimeoutNever    TimeoutType = "never"    // Never expires
)

// SessionTimeout represents a session timeout configuration
type SessionTimeout struct {
	Type  TimeoutType
	Value time.Duration // Only valid when Type == TimeoutDuration
}

// SessionCache represents the cached session data
type SessionCache struct {
	Key          string    `json:"key"`  // Base64 encoded derived key
	Salt         string    `json:"salt"` // Base64 encoded salt
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`     // Expiration time (zero for never/restart)
	TimeoutType  string    `json:"timeout_type"`   // "duration", "restart", "never"
	BootID       string    `json:"boot_id"`        // System boot ID (for restart type)
	DataPathHash string    `json:"data_path_hash"` // Hash of data path for validation
	SessionID    string    `json:"session_id"`     // Unique session ID for audit
}

// AuditEventType defines the type of audit event
type AuditEventType string

const (
	AuditSessionStart    AuditEventType = "session_start"
	AuditSessionExpire   AuditEventType = "session_expire"
	AuditSessionClear    AuditEventType = "session_clear"
	AuditSessionValidate AuditEventType = "session_validate"
	AuditAuthSuccess     AuditEventType = "auth_success"
	AuditAuthFailure     AuditEventType = "auth_failure"
)

// AuditEntry represents a single audit log entry
type AuditEntry struct {
	Timestamp   time.Time      `json:"timestamp"`
	EventType   AuditEventType `json:"event_type"`
	SessionID   string         `json:"session_id"`
	TimeoutType string         `json:"timeout_type,omitempty"`
	Duration    string         `json:"duration,omitempty"`
	Success     bool           `json:"success"`
	Message     string         `json:"message"`
	Hostname    string         `json:"hostname"`
	Username    string         `json:"username"`
}
