package provider

import (
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
