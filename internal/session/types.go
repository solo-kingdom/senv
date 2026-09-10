package session

import "time"

// TimeoutType defines the type of session timeout
type TimeoutType string

const (
	TimeoutDuration TimeoutType = "duration" // Fixed duration (e.g., 8h, 1d)
	TimeoutRestart  TimeoutType = "restart"  // Until system restart
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
	ExpiresAt    time.Time `json:"expires_at"`     // Expiration time (zero for restart)
	TimeoutType  string    `json:"timeout_type"`   // "duration", "restart" (legacy "never" is rejected as expired)
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
	AuditMCPRevocation   AuditEventType = "mcp_session_revoked"
	AuditAuthSuccess     AuditEventType = "auth_success"
	AuditAuthFailure     AuditEventType = "auth_failure"
	AuditClientBlocked   AuditEventType = "client_blocked"

	// 业务操作事件（op-audit）：target 只到 kind/group/key 或文件名粒度，
	// 绝不包含任何值或明文内容。
	AuditOpEnv         AuditEventType = "op_env"
	AuditOpText        AuditEventType = "op_text"
	AuditOpConfig      AuditEventType = "op_config"
	AuditOpInstall     AuditEventType = "op_install"
	AuditOpUninstall   AuditEventType = "op_uninstall"
	AuditOpSync        AuditEventType = "op_sync"
	AuditOpConflict    AuditEventType = "op_conflict"
	AuditOpRestore     AuditEventType = "op_restore"
	AuditOpSSHKey      AuditEventType = "op_ssh_keypair"
	AuditOpSSHHost     AuditEventType = "op_ssh_host"
	AuditOpLLMProvider AuditEventType = "op_llm_provider"
	AuditOpLLMSwitch   AuditEventType = "op_llm_switch"
	AuditOpMCPServer   AuditEventType = "op_mcp_server"
	AuditOpMCPExport   AuditEventType = "op_mcp_export"
)

// AuditEntry represents a single audit log entry
type AuditEntry struct {
	Timestamp   time.Time      `json:"timestamp"`
	EventType   AuditEventType `json:"event_type"`
	SessionID   string         `json:"session_id,omitempty"`
	Target      string         `json:"target,omitempty"` // 业务操作目标（kind/group/key、文件名）；会话事件为空
	TimeoutType string         `json:"timeout_type,omitempty"`
	Duration    string         `json:"duration,omitempty"`
	Success     bool           `json:"success"`
	Message     string         `json:"message,omitempty"`
	Hostname    string         `json:"hostname"`
	Username    string         `json:"username"`
}
