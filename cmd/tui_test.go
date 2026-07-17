package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// stubPrompter returns a fixed password, ignoring the prompt text.
func stubPrompter(password string) passwordPrompter {
	return func(string) (string, error) {
		return password, nil
	}
}

// isolateSessionCache redirects session cache paths into temp dirs so tests
// neither read nor overwrite the developer's real session files.
// It also resets the process-local auth memo and treats stdin/stdout as
// interactive so password-path unit tests work under non-TTY CI.
func isolateSessionCache(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	clearAuthMemo()
	stdinIsTerminal = func() bool { return true }
	stdoutIsTerminal = func() bool { return true }
	prevPrompt := authPrompt
	t.Cleanup(func() {
		clearAuthMemo()
		stdinIsTerminal = defaultStdinIsTerminal
		stdoutIsTerminal = defaultStdoutIsTerminal
		authPrompt = prevPrompt
		activeAuthOpts = authOptions{}
	})
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
	isolateSessionCache(t)
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
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	_, _, _, err := getManagersAt(cfg, data, stubPrompter("wrong-secret"))
	if !errors.Is(err, errInvalidPassword) {
		t.Fatalf("expected errInvalidPassword, got %v", err)
	}
}

func TestTuiStartupCorrectPassword(t *testing.T) {
	isolateSessionCache(t)
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

func TestTuiStartupReusesSessionWithoutPrompt(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	timeout, err := session.ParseTimeout("1h")
	if err != nil || timeout == nil {
		t.Fatalf("parse timeout: %v", err)
	}
	sm := session.NewManager(cfg, data)
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	promptCalled := false
	failPrompter := func(string) (string, error) {
		promptCalled = true
		return "", errors.New("prompter should not be called when session is valid")
	}

	envMgr, textMgr, configMgr, err := getManagersAt(cfg, data, failPrompter)
	if err != nil {
		t.Fatalf("expected success with session, got %v", err)
	}
	if promptCalled {
		t.Fatal("password prompter must not be called when session cache is valid")
	}
	if envMgr == nil || textMgr == nil || configMgr == nil {
		t.Fatal("managers must not be nil")
	}
}

func TestTuiStartupPasswordDoesNotWriteSession(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	_, _, _, err := getManagersAt(cfg, data, stubPrompter("correct-secret"))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	sm := session.NewManager(cfg, data)
	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("password auth must not create session cache")
	}
}
