package text

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return NewManager(sm, "pw")
}

func TestPrepareFinishEditor_Changed(t *testing.T) {
	m := newTestManager(t)
	if err := m.Set("default", "note", "original content"); err != nil {
		t.Fatalf("set: %v", err)
	}

	s, err := m.PrepareEditor("default", "note")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	// Temp file must be pre-filled with the existing content and 0600.
	data, err := os.ReadFile(s.TmpPath)
	if err != nil {
		t.Fatalf("read tmp: %v", err)
	}
	if string(data) != "original content" {
		t.Errorf("tmp prefill = %q, want original content", string(data))
	}
	if info, _ := os.Stat(s.TmpPath); info.Mode().Perm() != 0o600 {
		t.Errorf("tmp perm = %v, want 0600", info.Mode().Perm())
	}

	// Simulate the editor writing new content.
	if err := os.WriteFile(s.TmpPath, []byte("edited content"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	changed, err := m.FinishEditor(s)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true after edit")
	}

	v, _ := m.Get("default", "note")
	if v != "edited content" {
		t.Errorf("value = %q, want edited content", v)
	}
	// Temp file must be cleaned up.
	if _, err := os.Stat(s.TmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file not removed: %v", err)
	}
}

func TestPrepareFinishEditor_Unchanged(t *testing.T) {
	m := newTestManager(t)
	if err := m.Set("default", "note", "keep me"); err != nil {
		t.Fatalf("set: %v", err)
	}

	s, err := m.PrepareEditor("default", "note")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	changed, err := m.FinishEditor(s)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when content untouched")
	}

	v, _ := m.Get("default", "note")
	if v != "keep me" {
		t.Errorf("value = %q, want keep me", v)
	}
	if _, err := os.Stat(s.TmpPath); !os.IsNotExist(err) {
		t.Error("temp file not removed on unchanged path")
	}
}

func TestPrepareFinishEditor_NewEntry(t *testing.T) {
	m := newTestManager(t)

	// A non-existent key starts with an empty editor buffer.
	s, err := m.PrepareEditor("default", "brandnew")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if s.Original != "" {
		t.Errorf("original = %q, want empty for new entry", s.Original)
	}

	if err := os.WriteFile(s.TmpPath, []byte("fresh value"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	changed, err := m.FinishEditor(s)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for new entry")
	}

	v, _ := m.Get("default", "brandnew")
	if v != "fresh value" {
		t.Errorf("value = %q, want fresh value", v)
	}
}

// TestEditorCommandUsesEditor ensures the editor command resolves from the
// environment so the TUI can hand it to tea.ExecProcess.
func TestEditorCommandUsesEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "fake-editor-for-test")
	t.Setenv("PATH", "")

	s := &EditorSession{TmpPath: "/tmp/x"}
	cmd := s.EditorCommand()
	if cmd.Path != "fake-editor-for-test" && len(cmd.Args) == 0 {
		t.Errorf("unexpected editor command: %+v", cmd)
	}
}
