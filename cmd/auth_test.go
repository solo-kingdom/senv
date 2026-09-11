package cmd

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
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

func TestAuthMemo_PreservesPlatformSessionStoreFailure(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Darwin defaults to the disk hatch when tmpfs is unproven")
	}
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	timeout, err := session.ParseTimeout("restart")
	if err != nil || timeout == nil {
		t.Fatalf("parse timeout: %v", err)
	}
	if err := session.NewManager(cfg, data).StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	// After a session exists, make the platform runtime store unreadable.
	// On Linux the hardened tmpfs store reports ErrNoSecureSessionStore.
	diskRuntime, err := os.MkdirTemp(".", "senv-disk-runtime-")
	if err != nil {
		t.Fatalf("create disk runtime: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(diskRuntime) })
	t.Setenv("XDG_RUNTIME_DIR", diskRuntime)

	// `eval "$(senv env export)"` runs with captured stdout, exactly the user
	// symptom reported by new shell startup.
	stdoutIsTerminal = func() bool { return false }
	activeAuthOpts = authOptions{requireStdoutTTY: true}
	t.Cleanup(func() { activeAuthOpts = authOptions{} })

	var prompts int
	prompt := countingPrompter("correct-secret", &prompts)
	_, err = resolveAuth(cfg, data, prompt)
	if !errors.Is(err, session.ErrNoSecureSessionStore) {
		t.Fatalf("expected platform store failure, got %v", err)
	}
	if prompts != 0 {
		t.Fatalf("platform store failure must not become a password prompt, got %d", prompts)
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

	to, err := session.ParseTimeout("restart")
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

func TestResolveAuthDoesNotPersistSessionByDefault(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")

	if _, err := resolveAuth(cfg, data, stubPrompter("correct-secret")); err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	cache, err := session.NewManager(cfg, data).LoadCache()
	if err != nil || cache != nil {
		t.Fatalf("default password auth must stay ephemeral: cache=%v err=%v", cache, err)
	}
}

func TestResolveAuthAutoStartOptInPersistsSession(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	store := storage.NewManager(cfg, data)
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	settings.Session.AutoStart = true
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	clearAuthMemo()

	if _, err := resolveAuth(cfg, data, stubPrompter("correct-secret")); err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	cache, err := session.NewManager(cfg, data).LoadCache()
	if err != nil || cache == nil {
		t.Fatalf("auto_start must persist a session: cache=%v err=%v", cache, err)
	}
}

// TestResolveAuthRootCauseMessages locks the shared re-auth vocabulary: each
// cause renders a root cause plus exactly one deterministic next action, and an
// interactive prompt is never offered for causes a password cannot fix.
func TestResolveAuthRootCauseMessages(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantCause  session.AuthRootCause
		wantAction string
		promptable bool
	}{
		{"expired prompts", session.ErrSessionExpired, session.AuthCauseExpired, "senv session start", true},
		{"restarted prompts", session.ErrSessionInvalidated, session.AuthCauseRestarted, "senv session start", true},
		{
			"vault changed prompts",
			fmt.Errorf("%w: %w", session.ErrSessionInvalidated, session.ErrSessionVaultChanged),
			session.AuthCauseVaultChanged,
			"senv session clear --all, then senv session start",
			true,
		},
		{"multiple cache not promptable", session.ErrSessionUnverifiable, session.AuthCauseUnreadable, "resolve the environment issue above, then retry", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := wrapAuthCause(tc.err)
			got, ok := session.ClassifyAuthCause(wrapped)
			if !ok || got != tc.wantCause {
				t.Fatalf("classify = (%q, %v), want (%q, true)", got, ok, tc.wantCause)
			}
			msg := wrapped.Error()
			if !strings.Contains(msg, string(tc.wantCause)) {
				t.Fatalf("message %q missing cause %q", msg, tc.wantCause)
			}
			if !strings.Contains(msg, tc.wantAction) {
				t.Fatalf("message %q missing action %q", msg, tc.wantAction)
			}
		})
	}
}

// TestAuthErrorNoSecretLeak asserts no rendered cause message carries key, salt,
// or password material.
func TestAuthErrorNoSecretLeak(t *testing.T) {
	secretKey := base64.StdEncoding.EncodeToString([]byte("super-secret-derived-key-material"))
	secretSalt := base64.StdEncoding.EncodeToString([]byte("super-secret-salt-material"))
	secrets := []string{secretKey, secretSalt, "correct-secret"}

	inputs := []error{
		session.ErrSessionExpired,
		session.ErrSessionInvalidated,
		fmt.Errorf("%w: %w", session.ErrSessionInvalidated, session.ErrSessionVaultChanged),
		session.ErrSessionUnverifiable,
		session.ErrSessionStaleMetadata,
		session.ErrSessionStaleKey,
	}
	for _, in := range inputs {
		msg := wrapAuthCause(in).Error()
		for _, secret := range secrets {
			if strings.Contains(msg, secret) {
				t.Fatalf("cause message %q leaked secret %q", msg, secret)
			}
		}
	}
}
