package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/crypto"
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPrepareFinishEdit_Changed(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "server: 8080\n")
	if err := m.Create("app", src, "/etc/app.conf"); err != nil {
		t.Fatalf("create: %v", err)
	}

	s, err := m.PrepareEdit("app")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if string(s.Original) != "server: 8080\n" {
		t.Errorf("original = %q, want server: 8080", string(s.Original))
	}

	// Simulate the editor changing the content.
	if err := os.WriteFile(s.TmpPath, []byte("server: 9090\n"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	changed, err := m.FinishEdit(s)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true after edit")
	}

	// Verify via export.
	out := filepath.Join(t.TempDir(), "out.conf")
	if err := m.Export("app", out); err != nil {
		t.Fatalf("export: %v", err)
	}
	data, _ := os.ReadFile(out)
	if string(data) != "server: 9090\n" {
		t.Errorf("exported = %q, want server: 9090", string(data))
	}
	if _, err := os.Stat(s.TmpPath); !os.IsNotExist(err) {
		t.Error("temp file not removed after finish")
	}
}

func TestPrepareFinishEdit_Unchanged(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "keep\n")
	m.Create("app", src, "/etc/app.conf")

	s, _ := m.PrepareEdit("app")
	changed, err := m.FinishEdit(s)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when content untouched")
	}
}

func TestPrepareEdit_NotFound(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.PrepareEdit("nope"); err == nil {
		t.Fatal("expected error for missing config")
	}
}

// TestEditorCommandResolvesEditor confirms the command uses $EDITOR so the TUI
// can pass it to tea.ExecProcess.
func TestEditorCommandResolvesEditor(t *testing.T) {
	t.Setenv("EDITOR", "fake-editor-for-test")
	s := &ConfigEditSession{TmpPath: "/tmp/x"}
	cmd := s.EditorCommand()
	if len(cmd.Args) == 0 || cmd.Args[0] != "fake-editor-for-test" {
		t.Errorf("unexpected editor command: %+v", cmd)
	}
}

// TestEditorCommandDefaultsToVim confirms vim is the fallback editor.
func TestEditorCommandDefaultsToVim(t *testing.T) {
	t.Setenv("EDITOR", "")
	s := &ConfigEditSession{TmpPath: "/tmp/x"}
	cmd := s.EditorCommand()
	if cmd.Args[0] != "vim" {
		t.Errorf("expected vim fallback, got %q", cmd.Args[0])
	}
}

func TestManagerWithKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Create with password manager.
	pwMgr := NewManager(sm, "pw")
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "server: 8080\n")
	if err := pwMgr.Create("app", src, "/etc/app.conf"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Read/write with key manager.
	metadata, err := sm.LoadMetadata()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		t.Fatalf("salt: %v", err)
	}
	key := crypto.DeriveKey("pw", salt)
	keyMgr := NewManagerWithKey(sm, key)

	out := filepath.Join(t.TempDir(), "out.conf")
	if err := keyMgr.Export("app", out); err != nil {
		t.Fatalf("export with key: %v", err)
	}
	data, _ := os.ReadFile(out)
	if string(data) != "server: 8080\n" {
		t.Errorf("exported = %q, want server: 8080", string(data))
	}

	s, err := keyMgr.PrepareEdit("app")
	if err != nil {
		t.Fatalf("prepare with key: %v", err)
	}
	if err := os.WriteFile(s.TmpPath, []byte("server: 9090\n"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	changed, err := keyMgr.FinishEdit(s)
	if err != nil {
		t.Fatalf("finish with key: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	out2 := filepath.Join(t.TempDir(), "out2.conf")
	if err := pwMgr.Export("app", out2); err != nil {
		t.Fatalf("export after key edit: %v", err)
	}
	data2, _ := os.ReadFile(out2)
	if string(data2) != "server: 9090\n" {
		t.Errorf("exported = %q, want server: 9090", string(data2))
	}
}
