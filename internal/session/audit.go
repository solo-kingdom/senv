package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLogger handles session audit logging
type AuditLogger struct {
	logPath string
	mu      sync.Mutex
	file    *os.File
	// writeFailedOnce 保证写失败只告警一次（best-effort 契约）
	writeFailedOnce bool
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(configPath string) (*AuditLogger, error) {
	// Respect HOME so tests and isolated invocations do not write to an
	// unrelated account database home directory.
	logDir := filepath.Join(configPath, "logs")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		logDir = filepath.Join(home, ".log", "senv")
	}

	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, "audit.log")

	// Open log file in append mode
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}

	return &AuditLogger{
		logPath: logPath,
		file:    file,
	}, nil
}

// AuditLogPath 返回审计日志文件路径；无法定位 HOME 时返回空串
func AuditLogPath() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".log", "senv", "audit.log")
	}
	return ""
}

// Log writes an audit entry to the log file
func (al *AuditLogger) Log(eventType AuditEventType, sessionID string, success bool, message string) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	entry := AuditEntry{
		Timestamp: time.Now(),
		EventType: eventType,
		SessionID: sessionID,
		Success:   success,
		Message:   message,
		Hostname:  hostname,
		Username:  username,
	}

	// Write as JSON line
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal audit entry: %w", err)
	}

	_, err = al.file.Write(append(data, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write audit log: %w", err)
	}

	return nil
}

// LogWithDetails writes an audit entry with additional details
func (al *AuditLogger) LogWithDetails(eventType AuditEventType, sessionID string, success bool, message string, timeoutType string, duration string) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	entry := AuditEntry{
		Timestamp:   time.Now(),
		EventType:   eventType,
		SessionID:   sessionID,
		TimeoutType: timeoutType,
		Duration:    duration,
		Success:     success,
		Message:     message,
		Hostname:    hostname,
		Username:    username,
	}

	// Write as JSON line
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal audit entry: %w", err)
	}

	_, err = al.file.Write(append(data, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write audit log: %w", err)
	}

	return nil
}

// Close closes the audit log file
func (al *AuditLogger) Close() error {
	if al.file != nil {
		return al.file.Close()
	}
	return nil
}

// LogOp 记录一条业务操作审计事件（best-effort）：写入失败仅向 stderr 告警
// 一次，不改变调用方的控制流与退出码。target 只允许 kind/group/key、文件名
// 等非敏感标识，值与明文内容不得进入本函数。
func (al *AuditLogger) LogOp(eventType AuditEventType, target string, success bool, detail string) error {
	if al == nil {
		return nil
	}
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}
	entry := AuditEntry{
		Timestamp: time.Now(),
		EventType: eventType,
		Target:    target,
		Success:   success,
		Message:   detail,
		Hostname:  hostname,
		Username:  username,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return al.warnWriteFailure(fmt.Errorf("marshal audit entry: %w", err))
	}
	al.mu.Lock()
	defer al.mu.Unlock()
	if _, err := al.file.Write(append(data, '\n')); err != nil {
		return al.warnWriteFailureLocked(fmt.Errorf("write audit log: %w", err))
	}
	al.writeFailedOnce = false
	return nil
}

// warnWriteFailure 首次失败时向 stderr 告警（已持锁路径用 Locked 变体）
func (al *AuditLogger) warnWriteFailure(err error) error {
	al.mu.Lock()
	defer al.mu.Unlock()
	return al.warnWriteFailureLocked(err)
}

func (al *AuditLogger) warnWriteFailureLocked(err error) error {
	if !al.writeFailedOnce {
		al.writeFailedOnce = true
		fmt.Fprintf(os.Stderr, "⚠ 审计日志写入失败（不影响本次操作）: %v\n", err)
	}
	return err
}

// Rotate rotates the audit log if it exceeds a certain size
// This is an optional feature for future enhancement
func (al *AuditLogger) Rotate(maxSize int64, maxBackups int) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	// Check file size
	info, err := al.file.Stat()
	if err != nil {
		return err
	}

	if info.Size() < maxSize {
		return nil // No rotation needed
	}

	// Close current file
	al.file.Close()

	// Rotate existing backups
	for i := maxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", al.logPath, i)
		newPath := fmt.Sprintf("%s.%d", al.logPath, i+1)
		os.Rename(oldPath, newPath)
	}

	// Rename current log to .1
	backupPath := fmt.Sprintf("%s.1", al.logPath)
	os.Rename(al.logPath, backupPath)

	// Open new log file
	file, err := os.OpenFile(al.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open new audit log: %w", err)
	}

	al.file = file
	return nil
}
