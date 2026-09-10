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
	TimeoutType  string    `json:"timeout_type"`   // "duration", "restart" (legacy "never" is adopted as restart)
	BootID       string    `json:"boot_id"`        // System boot ID (for restart type)
	DataPathHash string    `json:"data_path_hash"` // Hash of data path for validation
	SessionID    string    `json:"session_id"`     // Unique session ID for audit
	// TimeoutSeconds 是 duration 会话建立时的原始 timeout，用于滑动续期；
	// 旧 cache 缺失该字段时视为不可续期（保留原绝对到期），不影响复用。
	TimeoutSeconds int64 `json:"timeout_seconds,omitempty"`
}

// InvalidReason explains why a persistent session can no longer be reused.
type InvalidReason string

const (
	ReasonNone         InvalidReason = ""              // cache 有效
	ReasonExpired      InvalidReason = "expired"       // duration 的 expires_at 已过
	ReasonRestarted    InvalidReason = "restarted"     // restart/never 会话遇上 boot ID 变化
	ReasonVaultChanged InvalidReason = "vault-changed" // 缓存属于另一个 vault
	ReasonUnknownType  InvalidReason = "unknown-type"  // 无法解释的 timeout_type
	ReasonUnreadable   InvalidReason = "unreadable"    // 环境故障或缓存本身损坏
)

// SessionState is the externally reported state of the current vault's session.
type SessionState string

const (
	StateNoSession    SessionState = "no-session"
	StateActive       SessionState = "active"
	StateExpired      SessionState = "expired"
	StateInvalidated  SessionState = "invalidated"
	StateUnverifiable SessionState = "unverifiable"
)

// AuditEventType defines the type of audit event
type AuditEventType string

const (
	AuditSessionStart        AuditEventType = "session_start"
	AuditSessionExpire       AuditEventType = "session_expire"
	AuditSessionInvalidated  AuditEventType = "session_invalidated"
	AuditSessionUnverifiable AuditEventType = "session_unverifiable"
	AuditSessionClear        AuditEventType = "session_clear"
	AuditSessionValidate     AuditEventType = "session_validate"
	AuditMCPRevocation       AuditEventType = "mcp_session_revoked"
	AuditAuthSuccess         AuditEventType = "auth_success"
	AuditAuthFailure         AuditEventType = "auth_failure"
	AuditClientBlocked       AuditEventType = "client_blocked"

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
