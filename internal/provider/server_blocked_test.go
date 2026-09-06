package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// blockedServer 返回固定 403 client_blocked 响应的测试 server
func blockedServer(t *testing.T, calls *int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			atomic.AddInt64(calls, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"client_blocked"}`))
	}))
}

func TestServerClientMapsBlocked403(t *testing.T) {
	srv := blockedServer(t, nil)
	defer srv.Close()
	c := newServerClient(srv.URL, "tok")
	_, _, err := c.Pull(context.Background(), "main", 0)
	if !errors.Is(err, ErrClientBlocked) {
		t.Fatalf("Pull err = %v, want ErrClientBlocked", err)
	}
	if !strings.Contains(err.Error(), BlockedGuidance) {
		t.Errorf("error should carry guidance, got %q", err.Error())
	}
}

func TestServerClientOther403NotBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden for other reason"}`))
	}))
	defer srv.Close()
	c := newServerClient(srv.URL, "tok")
	_, _, err := c.Pull(context.Background(), "main", 0)
	if errors.Is(err, ErrClientBlocked) {
		t.Fatalf("non client_blocked 403 must not map to ErrClientBlocked: %v", err)
	}
}

func TestBlockedCallbackFiresOnce(t *testing.T) {
	srv := blockedServer(t, nil)
	defer srv.Close()
	var fired int64
	c := newServerClient(srv.URL, "tok")
	c.onBlocked = func() { atomic.AddInt64(&fired, 1) }
	for i := 0; i < 3; i++ {
		_, _, _ = c.Pull(context.Background(), "main", 0)
	}
	if atomic.LoadInt64(&fired) != 1 {
		t.Fatalf("onBlocked fired %d times, want exactly 1", fired)
	}
}

// TestServerProviderBlockedClearsSession 验证屏蔽响应触发本地解锁缓存清理：
// HOME 指向临时目录，审计文件应出现 client_blocked 事件。
func TestServerProviderBlockedClearsSession(t *testing.T) {
	srv := blockedServer(t, nil)
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, "cfg")
	dataDir := filepath.Join(home, "data")

	p := NewServerProvider(srv.URL, "tok", cfgDir, dataDir, "main")
	err := p.Pull()
	if !errors.Is(err, ErrClientBlocked) {
		t.Fatalf("Pull err = %v, want ErrClientBlocked", err)
	}

	auditPath := filepath.Join(home, ".log", "senv", "audit.log")
	data, readErr := os.ReadFile(auditPath)
	if readErr != nil {
		t.Fatalf("audit log missing: %v", readErr)
	}
	if !strings.Contains(string(data), "client_blocked") {
		t.Errorf("audit log should record client_blocked event, got %q", string(data))
	}
}

func TestRegisterClientHTTP(t *testing.T) {
	var gotCode, gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotCode, gotName = body["code"], body["name"]
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":"new-token","client":{"id":7,"name":"laptop"}}`))
	}))
	defer srv.Close()

	res, err := RegisterClient(context.Background(), srv.URL, "code-1", "laptop")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if res.Token != "new-token" || res.ClientName != "laptop" || res.ClientID != 7 {
		t.Errorf("res = %+v, want token new-token client 7 laptop", res)
	}
	if gotCode != "code-1" || gotName != "laptop" {
		t.Errorf("request body = code %q name %q", gotCode, gotName)
	}
}

func TestRegisterClientNameConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"该用户下已存在同名 client，请更换设备名"}`))
	}))
	defer srv.Close()
	_, err := RegisterClient(context.Background(), srv.URL, "code-1", "laptop")
	if err == nil || !strings.Contains(err.Error(), "同名") {
		t.Fatalf("err = %v, want name-conflict message", err)
	}
	if errors.Is(err, ErrClientBlocked) {
		t.Fatalf("conflict must not map to ErrClientBlocked")
	}
}
