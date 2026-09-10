package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// auditCall 是一次 recordAudit 的快照，用于断言事件类型/目标/成功位。
type auditCall struct {
	event   session.AuditEventType
	target  string
	success bool
	detail  string
}

type fakeAuditWriter struct {
	calls []auditCall
}

func (f *fakeAuditWriter) Record(event session.AuditEventType, target string, success bool, detail string) {
	f.calls = append(f.calls, auditCall{event, target, success, detail})
}

// newAuditTestManagers 返回一套真实 manager + 记录写入审计的 Managers。
func newAuditTestManagers(t *testing.T) (Managers, *fakeAuditWriter) {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	w := &fakeAuditWriter{}
	return Managers{
		Env:         env.NewManager(sm, "pw"),
		Text:        text.NewManager(sm, "pw"),
		Config:      config.NewManager(sm, "pw"),
		AuditWriter: w,
	}, w
}

// assertNoSecretLeak 断言审计快照里不含操作值的明文。
func assertNoSecretLeak(t *testing.T, w *fakeAuditWriter, secret string) {
	t.Helper()
	for _, c := range w.calls {
		if strings.Contains(c.target, secret) || strings.Contains(c.detail, secret) {
			t.Fatalf("audit entry leaked value %q: %#v", secret, c)
		}
	}
}

func TestEnvWritesRecordAuditWithoutValues(t *testing.T) {
	mgrs, w := newAuditTestManagers(t)
	tab := newEnvTab(mgrs)
	tab.SetSize(80, 20)
	flush(tab, tab.load())

	const secret = "s3cr3t-value"
	flush(tab, tab.doSet("default", "API_KEY", secret))
	flush(tab, tab.doAddGroup("prod"))
	flush(tab, tab.doDelete("default", "API_KEY"))

	want := []auditCall{
		{session.AuditOpEnv, "env:default:API_KEY", true, "set"},
		{session.AuditOpEnv, "env:group:prod", true, "add group"},
		{session.AuditOpEnv, "env:default:API_KEY", true, "delete"},
	}
	if len(w.calls) != len(want) {
		t.Fatalf("audit calls = %#v, want %#v", w.calls, want)
	}
	for i, c := range want {
		if w.calls[i] != c {
			t.Errorf("call %d = %#v, want %#v", i, w.calls[i], c)
		}
	}
	assertNoSecretLeak(t, w, secret)
}

func TestAuditRecordsFailures(t *testing.T) {
	mgrs, w := newAuditTestManagers(t)
	tab := newEnvTab(mgrs)
	tab.SetSize(80, 20)
	flush(tab, tab.load())

	// Deleting a key that does not exist must still leave a failure trace.
	msgs := runCmd(tab.doDelete("default", "MISSING"))
	if len(w.calls) != 1 {
		t.Fatalf("audit calls = %#v, want one failure entry", w.calls)
	}
	if got := w.calls[0]; got.success || got.event != session.AuditOpEnv || got.target != "env:default:MISSING" {
		t.Errorf("failure entry = %#v", got)
	}
	var sawErr bool
	for _, m := range msgs {
		if _, ok := m.(errMsg); ok {
			sawErr = true
		}
	}
	if !sawErr {
		t.Errorf("expected errMsg from failing delete, got %#v", msgs)
	}
}

func TestTextWritesRecordAudit(t *testing.T) {
	mgrs, w := newAuditTestManagers(t)
	tab := newTextTab(mgrs)
	tab.SetSize(80, 20)

	flushText(tab, tab.doAddGroup("notes"))
	if len(w.calls) != 1 || w.calls[0].target != "text:group:notes" || !w.calls[0].success {
		t.Fatalf("add group audit = %#v", w.calls)
	}
	runCmd(tab.doDelete("notes", "missing-key"))
	if len(w.calls) != 2 {
		t.Fatalf("audit calls = %#v", w.calls)
	}
	if got := w.calls[1]; got.event != session.AuditOpText || got.success || got.target != "text:notes:missing-key" {
		t.Errorf("delete failure audit = %#v", got)
	}
}

func TestConfigWritesRecordAudit(t *testing.T) {
	mgrs, w := newAuditTestManagers(t)
	tab := newConfigTab(mgrs)
	tab.SetSize(80, 20)

	src := writeSourceFile(t, "value")
	flushConfig(tab, tab.doCreate("app", src, "target.conf", "default", "demo"))
	flushConfig(tab, tab.doDelete("app"))

	if len(w.calls) != 2 {
		t.Fatalf("audit calls = %#v", w.calls)
	}
	want := []auditCall{
		{session.AuditOpConfig, "config:default:app", true, "create"},
		{session.AuditOpConfig, "config:app", true, "delete"},
	}
	for i, c := range want {
		if w.calls[i] != c {
			t.Errorf("call %d = %#v, want %#v", i, w.calls[i], c)
		}
	}
}

// TestAuditWriterNilIsNoOp 保证只读嵌入（无 AuditWriter）下写路径不 panic。
func TestAuditWriterNilIsNoOp(t *testing.T) {
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	tab := newEnvTab(Managers{Env: env.NewManager(sm, "pw")})
	tab.SetSize(80, 20)
	flush(tab, tab.doSet("default", "FOO", "bar"))
}
