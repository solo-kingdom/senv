// 可信代理来源 IP 解析（X-Real-IP / X-Forwarded-For）的单元与集成测试。
// store 允许为 nil：resolveRemoteIP 与限速判定都不触库（触库的用例单独建库）。
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
)

// newIPTestServer 构造指定信任策略的 Server（不触库）
func newIPTestServer(trustProxy bool) *Server {
	return New(nil, Options{AuthRateLimit: -1, TrustProxyHeaders: trustProxy})
}

// requestWith 构造指定对端与代理头的请求
func requestWith(remoteAddr string, headers map[string]string) *http.Request {
	req := httptest.NewRequest("GET", "/v1/vaults/main/metadata", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestResolveRemoteIP(t *testing.T) {
	loopback := "127.0.0.1:8443"
	external := "192.0.2.1:1234"

	cases := []struct {
		name       string
		trustProxy bool
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "信任关闭时代理头一律忽略",
			trustProxy: false,
			remoteAddr: loopback,
			headers:    map[string]string{"X-Real-IP": "203.0.113.7"},
			want:       "127.0.0.1",
		},
		{
			name:       "对端非 loopback 时即使信任开启也忽略代理头",
			trustProxy: true,
			remoteAddr: external,
			headers:    map[string]string{"X-Real-IP": "203.0.113.7"},
			want:       "192.0.2.1",
		},
		{
			name:       "loopback 对端采信 X-Real-IP",
			trustProxy: true,
			remoteAddr: loopback,
			headers:    map[string]string{"X-Real-IP": "203.0.113.7"},
			want:       "203.0.113.7",
		},
		{
			name:       "X-Real-IP 非法时回落 X-Forwarded-For 最左值",
			trustProxy: true,
			remoteAddr: loopback,
			headers:    map[string]string{"X-Real-IP": "not-an-ip", "X-Forwarded-For": "203.0.113.9, 10.0.0.1"},
			want:       "203.0.113.9",
		},
		{
			name:       "X-Real-IP 优先于 X-Forwarded-For",
			trustProxy: true,
			remoteAddr: loopback,
			headers:    map[string]string{"X-Real-IP": "203.0.113.7", "X-Forwarded-For": "203.0.113.9"},
			want:       "203.0.113.7",
		},
		{
			name:       "两个头都非法时回落连接对端",
			trustProxy: true,
			remoteAddr: loopback,
			headers:    map[string]string{"X-Real-IP": "999.1.1.1", "X-Forwarded-For": ";;"},
			want:       "127.0.0.1",
		},
		{
			name:       "IPv6 客户端地址被采信",
			trustProxy: true,
			remoteAddr: loopback,
			headers:    map[string]string{"X-Real-IP": "2001:db8::1"},
			want:       "2001:db8::1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newIPTestServer(tc.trustProxy)
			got := srv.resolveRemoteIP(requestWith(tc.remoteAddr, tc.headers))
			if got != tc.want {
				t.Errorf("resolveRemoteIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// 信任开启后，经同机代理进来的不同真实客户端各有独立限速窗口，
// 单个客户端的失败认证不再能把全部客户端锁在同一个窗口外。
func TestTrustedProxyRateLimitIsolation(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	srv := New(st, Options{AuthRateLimit: 1, TrustProxyHeaders: true})

	do := func(realIP string) *httptest.ResponseRecorder {
		req := requestWith("127.0.0.1:8443", map[string]string{"X-Real-IP": realIP})
		req.Header.Set("Authorization", "Bearer bogus")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("203.0.113.1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first failure from client A = %d, want 401", rec.Code)
	}
	if rec := do("203.0.113.2"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("failure from client B must not be limited by client A: %d, want 401", rec.Code)
	}
	if rec := do("203.0.113.1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second failure from client A = %d, want 429", rec.Code)
	}
	if rec := do("203.0.113.2"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second failure from client B = %d, want 429", rec.Code)
	}

	// 非法 X-Real-IP 不被采信成新窗口：回落到代理自身（127.0.0.1）的窗口，
	// 该窗口此前无失败，请求照常认证（401），即攻击者无法借垃圾头逃避计数
	if rec := do("not-an-ip"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("garbage X-Real-IP = %d, want 401 (fallback to proxy's own window)", rec.Code)
	}
}
