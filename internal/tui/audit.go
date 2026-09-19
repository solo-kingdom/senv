package tui

import "github.com/wii/senv/internal/session"

// AuditWriter records one business operation in the local operation audit.
// Implementations MUST be best-effort: a failure to write must never block or
// fail the operation being audited (see the operation-audit spec).
type AuditWriter interface {
	Record(eventType session.AuditEventType, target string, success bool, detail string)
}

// recordAudit writes a best-effort audit entry. A nil writer is a no-op, so
// limited integrations (tests, git mode) and reduced TUI embeddings keep
// working unchanged.
func recordAudit(mgr Managers, eventType session.AuditEventType, target string, success bool, detail string) {
	if mgr.AuditWriter == nil {
		return
	}
	mgr.AuditWriter.Record(eventType, target, success, detail)
}

// envTarget / textTarget / configTarget build the non-sensitive audit targets
// shared by the tabs; they never include values.
func envTarget(group, key string) string     { return "env:" + group + ":" + key }
func textTarget(group, key string) string    { return "text:" + group + ":" + key }
func backupTarget(group, key string) string  { return "backup:" + group + ":" + key }
func configTarget(group, name string) string { return "config:" + group + ":" + name }
