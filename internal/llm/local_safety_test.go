package llm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSwitchManagerFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	sm := NewSwitchManager(nil, "", "")
	if _, _, err := sm.Status(); err == nil {
		t.Fatal("Status() unexpectedly resolved paths without HOME")
	}
	if _, err := sm.Switch("claude-code", "main", []string{"m1"}, "m1", ""); err == nil {
		t.Fatal("Switch() unexpectedly resolved paths without HOME")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	for _, name := range []string{".claude", ".codex", ".config"} {
		if _, err := os.Stat(filepath.Join(wd, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("home fallback created %s in cwd: %v", name, err)
		}
	}
}

func TestSensitivePathsAreTightened(t *testing.T) {
	home := t.TempDir()
	pointerDir := filepath.Join(home, ".config", "senv")
	configDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(pointerDir, 0o755); err != nil {
		t.Fatalf("create pointer dir: %v", err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create agent dir: %v", err)
	}
	pointer := filepath.Join(pointerDir, "agent-pointers.json")
	if err := os.WriteFile(pointer, []byte(`{"version":1,"agents":{}}`), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	if err := SavePointers(pointer, &PointerFile{Version: 1}); err != nil {
		t.Fatalf("SavePointers() error = %v", err)
	}
	assertPrivateDir(t, pointerDir)
	assertPrivateFile(t, pointer)

	configPath := filepath.Join(configDir, "settings.json")
	tx, err := newConfigTransaction(configPath)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := tx.write(configPath, []byte(`{"model":"m1"}`)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := tx.commit(); err != nil {
		t.Fatalf("commit config: %v", err)
	}
	assertPrivateDir(t, configDir)
	assertPrivateFile(t, configPath)
}

func assertPrivateDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat dir %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir %s perm = %o, want 700", path, got)
	}
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file %s perm = %o, want 600", path, got)
	}
}
