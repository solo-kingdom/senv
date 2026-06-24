package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

// stubPrompter returns a fixed password, ignoring the prompt text.
func stubPrompter(password string) passwordPrompter {
	return func(string) (string, error) {
		return password, nil
	}
}

// newInitializedProject creates a temporary initialized project rooted at dir
// (config under dir/cfg, data under dir/data) secured by the given password.
func newInitializedProject(t *testing.T, dir, password string) (configPath, dataPath string) {
	t.Helper()
	configPath = filepath.Join(dir, "cfg")
	dataPath = filepath.Join(dir, "data")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	mgr := storage.NewManager(configPath, dataPath)
	if err := mgr.Initialize(password); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return configPath, dataPath
}

func TestTuiStartupNotInitialized(t *testing.T) {
	// Point at an empty temp dir: no metadata.json present.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cfg")
	data := filepath.Join(dir, "data")

	_, _, _, err := getManagersAt(cfg, data, stubPrompter("anything"))
	if !errors.Is(err, errNotInitialized) {
		t.Fatalf("expected errNotInitialized, got %v", err)
	}
}

func TestTuiStartupWrongPassword(t *testing.T) {
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	_, _, _, err := getManagersAt(cfg, data, stubPrompter("wrong-secret"))
	if !errors.Is(err, errInvalidPassword) {
		t.Fatalf("expected errInvalidPassword, got %v", err)
	}
}

func TestTuiStartupCorrectPassword(t *testing.T) {
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	envMgr, textMgr, configMgr, err := getManagersAt(cfg, data, stubPrompter("correct-secret"))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if envMgr == nil || textMgr == nil || configMgr == nil {
		t.Fatalf("managers must not be nil")
	}
}
