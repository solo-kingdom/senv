package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/provider"
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
	runtimeDir := t.TempDir()
	if runtime.GOOS == "linux" {
		var err error
		runtimeDir, err = os.MkdirTemp("/dev/shm", "senv-cmd-test-")
		if err != nil {
			t.Fatalf("create tmpfs runtime directory: %v", err)
		}
		if err := os.Chmod(runtimeDir, 0o700); err != nil {
			t.Fatalf("chmod tmpfs runtime directory: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
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

// TestTUISyncSourcePull 覆盖 TUI 后台拉取的三类结果：节流窗口内零网络跳过、
// refresh 绕过窗口后正常应用（Applied>0）、网络错误只进 Err 不退出进程。
func TestTUISyncSourcePull(t *testing.T) {
	t.Setenv("SENV_ALLOW_INSECURE_HTTP", "1")
	password := "tui-pull-password"
	fake := &fakeAutoSyncServer{entries: map[string]provider.Entry{}}
	server := httptest.NewServer(fake)
	defer server.Close()

	// 设备 A：写入并推送到 server。
	configA, dataA := t.TempDir(), t.TempDir()
	storeA := storage.NewManager(configA, dataA)
	if err := storeA.Initialize(password); err != nil {
		t.Fatal(err)
	}
	envA := env.NewManager(storeA, password)
	if err := envA.Set("default", "PULL_ME", "value-a"); err != nil {
		t.Fatal(err)
	}
	pA := provider.NewServerProvider(server.URL, "test-token", configA, dataA, "main")
	if _, err := pA.SyncWithReport(context.Background()); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	// 设备 B：bootstrap 建立节流时间戳，随后以测试路径接入同一 vault。
	configB, dataB := t.TempDir(), t.TempDir()
	pB := provider.NewServerProvider(server.URL, "test-token", configB, dataB, "main")
	if err := pB.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	withTestPaths(t, configB, dataB)
	storeB := storage.NewManager(configB, dataB)
	settings := storage.NewSettings()
	settings.Provider = storage.ProviderConfig{
		Type: provider.TypeServer, Address: server.URL, Token: "test-token", Vault: "main",
		SyncThrottle: "1h",
	}
	if err := storeB.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	src := newTUISyncSource()
	if src == nil {
		t.Fatal("expected a sync source in server mode with auto_sync on")
	}

	// 节流窗口内：零网络跳过，outcome 保持零值。
	fake.mu.Lock()
	pullsBefore := fake.pulls
	fake.mu.Unlock()
	if out := src.Pull(false); out.Err != nil || out.Applied != 0 || out.MetadataUpdated {
		t.Fatalf("throttled pull outcome = %+v, want zero", out)
	}
	fake.mu.Lock()
	if fake.pulls != pullsBefore {
		extra := fake.pulls - pullsBefore
		fake.mu.Unlock()
		t.Fatalf("throttled pull made %d extra pulls, want 0", extra)
	}
	fake.mu.Unlock()

	// 设备 A 再推一条；B 用 refresh 绕过节流窗口后台拉取。
	if err := envA.Set("default", "PULL_LATER", "value-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := pA.SyncWithReport(context.Background()); err != nil {
		t.Fatalf("push second entry: %v", err)
	}
	fake.mu.Lock()
	pullsBefore = fake.pulls
	fake.mu.Unlock()
	out := src.Pull(true)
	if out.Err != nil {
		t.Fatalf("refresh pull err: %v", out.Err)
	}
	if out.Applied != 1 {
		t.Fatalf("applied = %d, want 1", out.Applied)
	}
	fake.mu.Lock()
	if fake.pulls != pullsBefore+1 {
		extra := fake.pulls - pullsBefore
		fake.mu.Unlock()
		t.Fatalf("refresh pull made %d extra pulls, want 1", extra)
	}
	fake.mu.Unlock()
}

// TestTUISyncSourcePullErrorsStayInProcess 验证错误路径：网络失败与被屏蔽都
// 只反映在返回的 Err 上，不打印不退出；调用方可用 errors.Is 判别屏蔽。
func TestTUISyncSourcePullErrorsStayInProcess(t *testing.T) {
	// 审计写入走全局路径，重定向到临时目录避免污染真实环境。
	withTestPaths(t, t.TempDir(), t.TempDir())

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		url := server.URL
		server.Close() // 立即关闭：拉取必然连接失败
		src := &tuiSyncSource{sp: provider.NewServerProvider(url, "test-token", t.TempDir(), t.TempDir(), "main")}
		out := src.Pull(true)
		if out.Err == nil {
			t.Fatal("expected an error from an unreachable server")
		}
		if errors.Is(out.Err, provider.ErrClientBlocked) {
			t.Fatalf("unreachable server must not map to ErrClientBlocked: %v", out.Err)
		}
	})

	t.Run("client blocked", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"client_blocked"}`))
		}))
		defer server.Close()
		src := &tuiSyncSource{sp: provider.NewServerProvider(server.URL, "test-token", t.TempDir(), t.TempDir(), "main")}
		out := src.Pull(true)
		if out.Err == nil || !errors.Is(out.Err, provider.ErrClientBlocked) {
			t.Fatalf("blocked pull err = %v, want ErrClientBlocked", out.Err)
		}
		if calls == 0 {
			t.Fatal("expected the blocked server to be contacted")
		}
	})
}
