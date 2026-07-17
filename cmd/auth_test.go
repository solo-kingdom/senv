package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func countingPrompter(password string, count *int) passwordPrompter {
	return func(string) (string, error) {
		*count++
		return password, nil
	}
}

func TestAuthMemo_ReusesSuccessfulAuth(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	var prompts int
	prompt := countingPrompter("correct-secret", &prompts)

	auth1, err := resolveAuth(cfg, data, prompt)
	if err != nil {
		t.Fatalf("first resolveAuth: %v", err)
	}
	auth2, err := resolveAuth(cfg, data, prompt)
	if err != nil {
		t.Fatalf("second resolveAuth: %v", err)
	}
	if prompts != 1 {
		t.Fatalf("expected 1 prompt, got %d", prompts)
	}
	if auth1.password != auth2.password || auth1.password != "correct-secret" {
		t.Fatalf("memoized auth mismatch: %+v vs %+v", auth1, auth2)
	}
}

func TestAuthMemo_FailedAuthNotCached(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	var prompts int
	wrong := countingPrompter("wrong-secret", &prompts)
	_, err := resolveAuth(cfg, data, wrong)
	if !errors.Is(err, errInvalidPassword) {
		t.Fatalf("expected errInvalidPassword, got %v", err)
	}
	if prompts != 1 {
		t.Fatalf("expected 1 prompt after wrong password, got %d", prompts)
	}

	prompts = 0
	right := countingPrompter("correct-secret", &prompts)
	auth, err := resolveAuth(cfg, data, right)
	if err != nil {
		t.Fatalf("retry after wrong password: %v", err)
	}
	if prompts != 1 {
		t.Fatalf("expected 1 prompt on retry, got %d", prompts)
	}
	if auth.password != "correct-secret" {
		t.Fatalf("unexpected password %q", auth.password)
	}
}

func TestAuthMemo_SimulatesEnvAndTextManagers(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	var prompts int
	prompt := countingPrompter("correct-secret", &prompts)

	// Simulate getEnvManager + getTextManager (as resolveValue / export does).
	if _, err := resolveAuth(cfg, data, prompt); err != nil {
		t.Fatalf("env path: %v", err)
	}
	if _, err := resolveAuth(cfg, data, prompt); err != nil {
		t.Fatalf("text path: %v", err)
	}
	if prompts != 1 {
		t.Fatalf("expected single prompt across env+text, got %d", prompts)
	}
}

func TestAuthMemo_NonInteractiveNoPrompt(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	stdinIsTerminal = func() bool { return false }

	var prompts int
	prompt := countingPrompter("correct-secret", &prompts)
	_, err := resolveAuth(cfg, data, prompt)
	if !errors.Is(err, ErrNeedSession) {
		t.Fatalf("expected ErrNeedSession, got %v", err)
	}
	if prompts != 0 {
		t.Fatalf("prompter must not be called, got %d", prompts)
	}
	if !strings.Contains(err.Error(), "senv session start") {
		t.Fatalf("error should mention session start: %v", err)
	}
}

func TestAuthMemo_ExportStdoutNonTTYNoPrompt(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	stdoutIsTerminal = func() bool { return false }
	activeAuthOpts = authOptions{requireStdoutTTY: true}
	t.Cleanup(func() { activeAuthOpts = authOptions{} })

	var prompts int
	prompt := countingPrompter("correct-secret", &prompts)
	_, err := resolveAuth(cfg, data, prompt)
	if !errors.Is(err, ErrNeedSession) {
		t.Fatalf("expected ErrNeedSession, got %v", err)
	}
	if prompts != 0 {
		t.Fatalf("prompter must not be called, got %d", prompts)
	}
}

func TestExportIfSession_NoSessionSilent(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	useProjectPaths(t, cfg, data)

	var prompts int
	authPrompt = countingPrompter("correct-secret", &prompts)
	stdoutIsTerminal = func() bool { return false }

	envExportIfSession = true
	t.Cleanup(func() { envExportIfSession = false })

	out := captureStdout(t, func() {
		if err := envExportCmd.RunE(envExportCmd, nil); err != nil {
			t.Fatalf("export --if-session: %v", err)
		}
	})
	if out != "" {
		t.Fatalf("expected empty stdout, got %q", out)
	}
	if prompts != 0 {
		t.Fatalf("must not prompt, got %d", prompts)
	}
}

func TestExportIfSession_WithSessionExports(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	useProjectPaths(t, cfg, data)

	store := storage.NewManager(cfg, data)
	em := env.NewManager(store, "correct-secret")
	if err := em.Set("default", "FOO", "bar"); err != nil {
		t.Fatalf("set env: %v", err)
	}

	to, err := session.ParseTimeout("never")
	if err != nil || to == nil {
		t.Fatalf("parse timeout: %v", err)
	}
	if err := session.NewManager(cfg, data).StartSession("correct-secret", to); err != nil {
		t.Fatalf("start session: %v", err)
	}
	clearAuthMemo()

	var prompts int
	authPrompt = countingPrompter("should-not-be-used", &prompts)
	stdoutIsTerminal = func() bool { return false }

	envExportIfSession = true
	t.Cleanup(func() { envExportIfSession = false })

	out := captureStdout(t, func() {
		if err := envExportCmd.RunE(envExportCmd, nil); err != nil {
			t.Fatalf("export --if-session with session: %v", err)
		}
	})
	if !strings.Contains(out, "FOO") || !strings.Contains(out, "bar") {
		t.Fatalf("expected FOO=bar export, got %q", out)
	}
	if prompts != 0 {
		t.Fatalf("session path must not prompt, got %d", prompts)
	}
}

func TestExportSinglePrompt_WithTextRefNoSession(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	useProjectPaths(t, cfg, data)

	store := storage.NewManager(cfg, data)
	tm := text.NewManager(store, "correct-secret")
	if err := tm.Set("default", "SECRET", "from-text"); err != nil {
		t.Fatalf("set text: %v", err)
	}
	em := env.NewManager(store, "correct-secret")
	if err := em.Set("default", "API_KEY", "{{text:SECRET}}"); err != nil {
		t.Fatalf("set env: %v", err)
	}

	var prompts int
	authPrompt = countingPrompter("correct-secret", &prompts)
	// Interactive TTY so temporary password auth is allowed.
	stdinIsTerminal = func() bool { return true }
	stdoutIsTerminal = func() bool { return true }

	out := captureStdout(t, func() {
		if err := envExportCmd.RunE(envExportCmd, nil); err != nil {
			t.Fatalf("export: %v", err)
		}
	})
	if !strings.Contains(out, "from-text") {
		t.Fatalf("expected resolved text value, got %q", out)
	}
	if prompts != 1 {
		t.Fatalf("expected exactly 1 password prompt, got %d", prompts)
	}

	sm := session.NewManager(cfg, data)
	if _, err := sm.GetCachedKey(); err == nil {
		t.Fatal("export must not write session cache")
	}
}

func TestExport_CapturedStdoutNoSessionNeedSession(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	useProjectPaths(t, cfg, data)

	var prompts int
	authPrompt = countingPrompter("correct-secret", &prompts)
	stdinIsTerminal = func() bool { return true } // eval often keeps stdin TTY
	stdoutIsTerminal = func() bool { return false }

	err := envExportCmd.RunE(envExportCmd, nil)
	if !errors.Is(err, ErrNeedSession) {
		t.Fatalf("expected ErrNeedSession, got %v", err)
	}
	if prompts != 0 {
		t.Fatalf("must not prompt under captured stdout, got %d", prompts)
	}
}

// useProjectPaths points getConfigPath/getDataPath at a test project.
func useProjectPaths(t *testing.T, cfg, data string) {
	t.Helper()
	prevCfg := configPathFn
	prevData := dataPath
	configPathFn = func() string { return cfg }
	dataPath = data
	t.Cleanup(func() {
		configPathFn = prevCfg
		dataPath = prevData
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-done
}
