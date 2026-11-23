package provider

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestNewDefaultIsGit(t *testing.T) {
	p, err := New(Config{GitPath: t.TempDir()})
	if err != nil {
		t.Fatalf("New() with empty type should default to git, got error: %v", err)
	}
	if _, ok := p.(*GitProvider); !ok {
		t.Fatalf("New() with empty type should return *GitProvider, got %T", p)
	}
}

func TestNewExplicitGit(t *testing.T) {
	p, err := New(Config{Type: TypeGit, GitPath: t.TempDir()})
	if err != nil {
		t.Fatalf("New(git) error: %v", err)
	}
	if _, ok := p.(*GitProvider); !ok {
		t.Fatalf("New(git) should return *GitProvider, got %T", p)
	}
}

func TestNewGitMissingPath(t *testing.T) {
	_, err := New(Config{Type: TypeGit})
	if err == nil {
		t.Fatal("New(git) without path should fail")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("error should mention provider type git, got: %v", err)
	}
}

func TestNewServerMissingParams(t *testing.T) {
	// 缺少 address 与 token 时必须报明确错误，且绝不静默回退 git
	_, err := New(Config{Type: TypeServer, GitPath: t.TempDir()})
	if err == nil {
		t.Fatal("New(server) without address/token should fail")
	}
	if !strings.Contains(err.Error(), "address") || !strings.Contains(err.Error(), "token") {
		t.Errorf("error should list missing items, got: %v", err)
	}
}

func TestNewServerMissingTokenOnly(t *testing.T) {
	_, err := New(Config{Type: TypeServer, ServerAddress: "https://senv.example.com"})
	if err == nil {
		t.Fatal("New(server) without token should fail")
	}
	if !strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "address,") {
		t.Errorf("error should point at missing token, got: %v", err)
	}
}

func TestNewServerProvider(t *testing.T) {
	// 参数完整时构造 server provider
	p, err := New(Config{Type: TypeServer, ServerAddress: "https://senv.example.com", ServerToken: "tok"})
	if err != nil {
		t.Fatalf("New(server) with full config: %v", err)
	}
	if _, ok := p.(*ServerProvider); !ok {
		t.Fatalf("New(server) should return *ServerProvider, got %T", p)
	}
}

func TestNewUnknownType(t *testing.T) {
	_, err := New(Config{Type: "svn", GitPath: t.TempDir()})
	if err == nil {
		t.Fatal("New(unknown) should fail")
	}
	if !strings.Contains(err.Error(), "svn") {
		t.Errorf("error should mention the unknown type, got: %v", err)
	}
}

func TestValidateServerAddress(t *testing.T) {
	t.Setenv(allowInsecureHTTPEnv, "")
	tests := []struct {
		name    string
		address string
		wantErr bool
		// wantInErr 为空时仅校验是否报错;非空时错误须包含该子串
		wantInErr string
	}{
		{"https 通过", "https://senv.example.com", false, ""},
		{"https 带端口通过", "https://senv.example.com:8443", false, ""},
		{"http 默认拒绝", "http://senv.example.com", true, "SENV_ALLOW_INSECURE_HTTP"},
		{"无 scheme 拒绝", "senv.example.com:8080", true, "https://"},
		{"空串拒绝", "", true, "https://"},
		{"非法 URL 拒绝", "https://[::1", true, "https://"},
		{"其他 scheme 拒绝", "ftp://senv.example.com", true, "https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServerAddress(tt.address)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateServerAddress(%q) err = %v, wantErr %v", tt.address, err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), tt.address) {
					t.Errorf("error must contain the address, got: %v", err)
				}
				if tt.wantInErr != "" && !strings.Contains(err.Error(), tt.wantInErr) {
					t.Errorf("error must contain %q, got: %v", tt.wantInErr, err)
				}
			}
		})
	}
}

func TestValidateServerAddressHTTPExemption(t *testing.T) {
	t.Setenv(allowInsecureHTTPEnv, "1")
	if err := ValidateServerAddress("http://10.0.0.5:8080"); err != nil {
		t.Fatalf("http with explicit exemption should pass, got: %v", err)
	}
}

func TestIsInternalHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		// 字面量 IP：私网 / 回环 / 链路本地 / ULA / CGNAT / 未指定
		{"10.0.0.5", true},
		{"172.16.0.1", true},
		{"192.168.1.10", true},
		{"100.64.0.7", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fd00::1", true},
		{"fe80::1%eth0", true},
		{"::ffff:192.168.1.10", true},
		// 本地保留域名
		{"localhost", true},
		{"api.localhost", true},
		{"senv.lan", true},
		{"SENV.LAN.", true},
		{"nas.local", true},
		{"vault.internal", true},
		{"router.home.arpa", true},
		// 公网或无法判定为内网的
		{"senv.example.com", false},
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
		{"notlan.com", false},
		{"lan", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isInternalHost(tt.host); got != tt.want {
			t.Errorf("isInternalHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

// captureStderr 捕获 fn 执行期间写到 os.Stderr 的内容。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	r.Close()
	return string(out)
}

func TestValidateServerAddressWarnsOnlyForPublicHTTP(t *testing.T) {
	// 豁免生效后，只有链路可能出内网的地址才需要告警；内网/本机地址不提示。
	t.Setenv(allowInsecureHTTPEnv, "1")
	tests := []struct {
		address  string
		wantWarn bool
	}{
		{"http://senv.lan:8080", false},
		{"http://10.0.0.5:8080", false},
		{"http://192.168.1.10", false},
		{"http://100.64.0.7:8080", false},
		{"http://127.0.0.1:8080", false},
		{"http://[::1]:8080", false},
		{"http://[fd00::1]:8080", false},
		{"http://localhost:8080", false},
		{"http://senv.example.com", true},
		{"http://8.8.8.8:8080", true},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			var err error
			out := captureStderr(t, func() { err = ValidateServerAddress(tt.address) })
			if err != nil {
				t.Fatalf("ValidateServerAddress(%q) with exemption: %v", tt.address, err)
			}
			if got := strings.Contains(out, "明文 http"); got != tt.wantWarn {
				t.Fatalf("ValidateServerAddress(%q) warn = %v, want %v; stderr=%q", tt.address, got, tt.wantWarn, out)
			}
		})
	}
}

func TestNewServerHTTPExemptionEndToEnd(t *testing.T) {
	// 豁免开启时,http 地址可经统一构造入口成功构造(内网场景)。
	t.Setenv(allowInsecureHTTPEnv, "1")
	p, err := New(Config{
		Type:          TypeServer,
		ServerAddress: "http://127.0.0.1:8080",
		ServerToken:   "tok",
		ConfigPath:    t.TempDir(),
		DataPath:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New(server) with http + exemption: %v", err)
	}
	if _, ok := p.(*ServerProvider); !ok {
		t.Fatalf("expected *ServerProvider, got %T", p)
	}
}
