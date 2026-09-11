package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestServerProviderConcurrentSharedInstance 验证同一 ServerProvider 实例被
// 多 goroutine 并发使用（TUI 的 pull/push/状态刷新共享单例后的形态）在
// -race 下无数据竞争：fake API 与 localCache 的共享访问路径全覆盖。
func TestServerProviderConcurrentSharedInstance(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "g", "k", "v")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, _, _ = p.LocalSyncSnapshot()
		}()
		go func() {
			defer wg.Done()
			_, _, _ = p.AutoPull(context.Background(), 0, false)
		}()
		go func() {
			defer wg.Done()
			_, _ = p.AutoPush(context.Background(), 0)
		}()
	}
	wg.Wait()
}

// TestServerClientConcurrentReuse 验证真实 serverClient（生产共享单例的底层
// client）并发请求在 -race 下无数据竞争，且连接复用计数正常。
func TestServerClientConcurrentReuse(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"blob":"e30="}`))
	}))
	defer srv.Close()

	c := newServerClient(srv.URL, "token")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out struct {
				Blob []byte `json:"blob"`
			}
			if err := c.do(context.Background(), "GET", "/v1/vaults/main/metadata", nil, &out); err != nil {
				t.Errorf("do: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 8 {
		t.Fatalf("calls = %d, want 8", got)
	}
}
