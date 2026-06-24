package tui

import (
	"errors"
	"os"
	"testing"
)

func TestEmptyStateHintsRender(t *testing.T) {
	// Env tab with a group that has no variables.
	tab := envTabWith() // default group, no items
	if got := tab.View(); !contains(got, "no variables") {
		t.Errorf("env empty state hint missing: %q", got)
	}

	// Text tab with a group that has no blocks.
	tt := newTextTab(Managers{})
	tt.loaded = true
	tt.groups = []textGroupRow{{name: "default"}}
	tt.itemsByGroup = map[string][]textItemRow{"default": nil}
	tt.SetSize(80, 20)
	if got := tt.View(); !contains(got, "no text blocks") {
		t.Errorf("text empty state hint missing: %q", got)
	}

	// Config tab with no configs.
	ct := newConfigTab(Managers{})
	ct.loaded = true
	ct.items = nil
	ct.SetSize(80, 20)
	if got := ct.View(); !contains(got, "no configuration files") {
		t.Errorf("config empty state hint missing: %q", got)
	}
}

func TestTextEditorFailureCleansUpAndPreservesValue(t *testing.T) {
	mgr := newTestTextManager(t)
	if err := mgr.Set("default", "note", "original"); err != nil {
		t.Fatalf("set: %v", err)
	}

	tab := newTextTab(Managers{Text: mgr})
	session, err := mgr.PrepareEditor("default", "note")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	// Simulate the editor failing to run (e.g. EDITOR=/no/such/editor).
	msg := tab.finishAfterEdit(session, errors.New("editor not found"))
	if _, ok := msg.(errMsg); !ok {
		t.Fatalf("expected errMsg on editor failure, got %T", msg)
	}

	// Temp file must be cleaned up despite the failure.
	if _, err := os.Stat(session.TmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file not cleaned up after editor failure: %v", err)
	}

	// The stored value must be unchanged.
	v, _ := mgr.Get("default", "note")
	if v != "original" {
		t.Errorf("value changed after failed edit: %q, want original", v)
	}
}

func TestConfigEditorFailureCleansUp(t *testing.T) {
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "server: 8080\n")
	if err := mgr.Create("app", src, "/etc/app.conf"); err != nil {
		t.Fatalf("create: %v", err)
	}

	tab := newConfigTab(Managers{Config: mgr})
	session, err := mgr.PrepareEdit("app")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	msg := tab.finishAfterEdit(session, errors.New("exit status 1"))
	if _, ok := msg.(errMsg); !ok {
		t.Fatalf("expected errMsg on editor failure, got %T", msg)
	}
	if _, err := os.Stat(session.TmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file not cleaned up after editor failure: %v", err)
	}

	// Content unchanged.
	out := joinTemp(t)
	_ = mgr.Export("app", out)
	data, _ := os.ReadFile(out)
	if string(data) != "server: 8080\n" {
		t.Errorf("content changed after failed edit: %q", string(data))
	}
}

// joinTemp returns a temp output path for export verification.
func joinTemp(t *testing.T) string {
	t.Helper()
	return tempPath(t, "out.conf")
}

func tempPath(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	return dir + "/" + name
}
