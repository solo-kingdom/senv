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
