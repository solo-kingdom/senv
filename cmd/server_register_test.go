package cmd

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	if settings.Provider.Type != "server" || settings.Provider.Token != "reg-token-1" || settings.Provider.Address != srv.URL {
		t.Errorf("provider settings = %+v, want server/reg-token-1/%s", settings.Provider, srv.URL)
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
}
