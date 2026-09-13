package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/storage"
)

func resetServerRegisterFlags(t *testing.T) {
	t.Helper()
	prevAddr, prevCode, prevName, prevVault := serverRegisterAddress, serverRegisterCode, serverRegisterName, serverRegisterVault
	t.Cleanup(func() {
		serverRegisterAddress, serverRegisterCode, serverRegisterName, serverRegisterVault = prevAddr, prevCode, prevName, prevVault
	})
}

// newRegisterTestServer 返回固定 201 注册响应的测试 server
func newRegisterTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":"reg-token-1","client":{"id":1,"name":"test-dev"}}`))
	}))
}

func TestServerRegisterWritesSettings(t *testing.T) {
	resetServerRegisterFlags(t)
	srv := newRegisterTestServer(t)
	defer srv.Close()
	t.Setenv("SENV_ALLOW_INSECURE_HTTP", "1") // httptest 明文 http，测试内豁免

	cfg := t.TempDir()
	prevCfg := configPathFn
	configPathFn = func() string { return cfg }
	t.Cleanup(func() { configPathFn = prevCfg })

	serverRegisterAddress = srv.URL
	serverRegisterCode = "code-abc"
	serverRegisterName = "test-dev"
	serverRegisterVault = "main"

	if err := runServerRegister(&cobra.Command{}, nil); err != nil {
		t.Fatalf("runServerRegister: %v", err)
	}

	settings, err := storage.NewManager(cfg, filepath.Join(cfg, "data")).LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	// settings.json 只保留非敏感字段；token 绝不能出现在会被 git 同步的文件里
	if settings.Provider.Type != "server" || settings.Provider.Address != srv.URL {
		t.Errorf("provider settings = %+v, want server/%s", settings.Provider, srv.URL)
	}
	if settings.Provider.Token != "" {
		t.Errorf("settings must not embed the token (git-synced file), got %q", settings.Provider.Token)
	}
	mgr := storage.NewManager(cfg, filepath.Join(cfg, "data"))
	token, err := mgr.LoadServerToken()
	if err != nil {
		t.Fatalf("LoadServerToken: %v", err)
	}
	if token != "reg-token-1" {
		t.Errorf("server-token.json = %q, want reg-token-1", token)
	}
	// .gitignore 必须覆盖 token 文件
	ignore, err := os.ReadFile(filepath.Join(cfg, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(ignore), storage.ServerTokenFile) {
		t.Errorf(".gitignore missing %s:\n%s", storage.ServerTokenFile, ignore)
	}
}

func TestApplyRegisteredServerDefaults(t *testing.T) {
	prevAddr, prevToken, prevVault := initServerAddress, initServerToken, initServerVault
	t.Cleanup(func() { initServerAddress, initServerToken, initServerVault = prevAddr, prevToken, prevVault })

	cfg := t.TempDir()
	mgr := storage.NewManager(cfg, filepath.Join(cfg, "data"))
	settings := storage.NewSettings()
	settings.Provider = storage.ProviderConfig{
		Type:    "server",
		Address: "https://senv.example.com",
		Token:   "stored-token",
		Vault:   "team-vault",
	}
	if err := mgr.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	// 显式 flag 优先：不动
	initServerAddress = "https://explicit.example.com"
	applyRegisteredServerDefaults(mgr)
	if initServerAddress != "https://explicit.example.com" {
		t.Errorf("explicit flag must win, got %q", initServerAddress)
	}

	// 未显式给地址：回落到已注册配置
	initServerAddress, initServerToken, initServerVault = "", "", ""
	applyRegisteredServerDefaults(mgr)
	if initServerAddress != "https://senv.example.com" || initServerToken != "stored-token" || initServerVault != "team-vault" {
		t.Errorf("defaults = %q/%q/%q, want registered provider config", initServerAddress, initServerToken, initServerVault)
	}

	// 回落过程应把遗留的 settings 内嵌 token 迁移到机器本地文件并清空字段
	token, err := mgr.LoadServerToken()
	if err != nil || token != "stored-token" {
		t.Errorf("LoadServerToken = %q (%v), want stored-token", token, err)
	}
	after, err := mgr.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings after migration: %v", err)
	}
	if after.Provider.Token != "" {
		t.Errorf("settings token must be cleared after fallback read, got %q", after.Provider.Token)
	}
}
