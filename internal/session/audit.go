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
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(dataPath string) (*AuditLogger, error) {
	logDir := filepath.Join(dataPath, "logs")
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
