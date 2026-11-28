package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/store"
)

func doLANRequest(t *testing.T, h http.Handler, method, target, remoteAddr, host string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if host != "" {
		req.Host = host
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLANOnlyAndSyncTokenForbidden(t *testing.T) {
	st := newFakeStore()
	_, validToken := st.addUser("alice")
	srv := New(st)

	// 1. 公网 IP / 公网 Host / 代理头混入公网 IP 一律 403 (fail-closed)
	cases403 := []struct {
		name   string
		remote string
		host   string
		hdrs   map[string]string
	}{
		{"public remote", "8.8.8.8:12345", "localhost:8080", nil},
		{"public host", "127.0.0.1:12345", "senv.example.com", nil},
		{"public X-Real-IP", "127.0.0.1:12345", "localhost", map[string]string{"X-Real-IP": "1.2.3.4"}},
		{"public X-Forwarded-For", "127.0.0.1:12345", "localhost", map[string]string{"X-Forwarded-For": "10.0.0.2, 203.0.113.9"}},
		{"public X-Forwarded-Host", "127.0.0.1:12345", "localhost", map[string]string{"X-Forwarded-Host": "senv.example.com"}},
		{"sync bearer token on /stats", "127.0.0.1:12345", "localhost", map[string]string{"Authorization": "Bearer " + validToken}},
		{"sync bearer token on /stats/api/overview", "127.0.0.1:12345", "localhost", map[string]string{"Authorization": "Bearer bogus"}},
		{"X-API-Key on /stats/api/routes", "127.0.0.1:12345", "localhost", map[string]string{"X-API-Key": "secret"}},
	}
	for _, tc := range cases403 {
		for _, path := range []string{"/stats", "/stats/api/overview", "/stats/api/api-keys"} {
			rec := doLANRequest(t, srv, http.MethodGet, path, tc.remote, tc.host, tc.hdrs)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s (%s): got %d, want 403 (body=%s)", tc.name, path, rec.Code, rec.Body.String())
			}
		}
	}

	// 2. 非法自定义 CIDR 触发 fail-closed
	badSrv := New(st, Options{LANAllowedCIDRs: []string{"not-a-cidr"}})
	if rec := doLANRequest(t, badSrv, http.MethodGet, "/stats/api/overview", "127.0.0.1:12345", "localhost", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("invalid CIDR must fail-closed 403, got %d", rec.Code)
	}

	// 3. 内网 IP + 内网 Host + 无 Token 放行 200
	for _, host := range []string{"localhost:8080", "senv.lan", "192.168.1.10:8080"} {
		rec := doLANRequest(t, srv, http.MethodGet, "/stats/api/overview", "192.168.1.20:54321", host, map[string]string{
			"X-Real-IP":       "10.0.0.5",
			"X-Forwarded-For": "100.64.0.10, 172.16.0.2",
		})
		if rec.Code != http.StatusOK {
			t.Errorf("valid LAN host=%s got %d, want 200 (%s)", host, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
	}
}

func TestStatsEmbeddedHTMLAndNoExternalDeps(t *testing.T) {
	srv := New(newFakeStore())
	rec := doLANRequest(t, srv, http.MethodGet, "/stats", "127.0.0.1:1000", "localhost", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"senv-server 接口访问统计", "概览", "API Key 专项", "__invalid__", "/stats/api/overview"} {
		if !strings.Contains(body, want) {
			t.Errorf("embedded HTML missing %q", want)
		}
	}
	if strings.Contains(body, "https://") || strings.Contains(body, "http://") {
		t.Errorf("embedded HTML must have zero external URL dependencies")
	}
}

func TestStatsCollectionNormalizationAndSanitization(t *testing.T) {
	st := newFakeStore()
	_, tok := st.addUser("alice")
	srv := New(st)

	// 1. 正常认证请求（带参数路径）
	r1 := doRequest(t, srv, http.MethodGet, "/v1/vaults/personal-secret/metadata?token=leak-query-secret", tok, nil)
	if r1.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty vault, got %d", r1.Code)
	}
	// 2. 未匹配路由 404 -> /_unmatched
	r2 := doRequest(t, srv, http.MethodGet, "/wp-admin/install.php?key=leak-key-123", "", nil)
	if r2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unmatched, got %d", r2.Code)
	}
	// 3. 降噪跳过路径
	doRequest(t, srv, http.MethodGet, "/healthz", "", nil)
	doRequest(t, srv, http.MethodGet, "/assets/app.js", "", nil)
	doLANRequest(t, srv, http.MethodGet, "/stats", "127.0.0.1:1", "localhost", nil)
	doLANRequest(t, srv, http.MethodGet, "/stats/api/overview", "127.0.0.1:1", "localhost", nil)

	st.mu.Lock()
	events := append([]store.AccessEvent(nil), st.access...)
	st.mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 recorded events (noise skipped), got %d: %+v", len(events), events)
	}
	if events[0].RouteTemplate != "/v1/vaults/{vault}/metadata" || events[0].AuthType != "api_key" || events[0].StatusCode != http.StatusNotFound {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].RouteTemplate != "/_unmatched" || events[1].AuthType != "anonymous" || events[1].StatusCode != http.StatusNotFound {
		t.Errorf("event[1] = %+v", events[1])
	}
	// 脱敏断言：任何字段不含明文 token 或 query secret
	for _, e := range events {
		joined := e.Path + e.RouteTemplate + e.Reason + e.IP
		if strings.Contains(joined, tok) || strings.Contains(joined, "leak-query-secret") || strings.Contains(joined, "leak-key-123") {
			t.Errorf("secret leaked in event: %+v", e)
		}
	}

	// 4. API_STATS_ENABLED=false 一键关闭采集
	disabled := false
	srvOff := New(st, Options{APIStatsEnabled: &disabled})
	doRequest(t, srvOff, http.MethodGet, "/v1/vaults/main/metadata", tok, nil)
	st.mu.Lock()
	after := len(st.access)
	st.mu.Unlock()
	if after != 2 {
		t.Fatalf("APIStatsEnabled=false should not record events, got %d", after)
	}
}

func TestStatsAsyncQueueDropNonBlocking(t *testing.T) {
	st := &blockingAccessStore{fakeStore: newFakeStore(), gate: make(chan struct{})}
	srv := New(st, Options{AsyncAccessLogBuffer: 1})
	defer func() {
		close(st.gate)
		srv.Shutdown(context.Background())
	}()

	for i := 0; i < 10; i++ {
		start := time.Now()
		doRequest(t, srv, http.MethodGet, "/v1/vaults/main/metadata", "bad-token", nil)
		if time.Since(start) > 100*time.Millisecond {
			t.Fatalf("request %d blocked on full async queue", i)
		}
	}
	if dropped := srv.DroppedAccessLogCount(); dropped == 0 {
		t.Fatalf("expected dropped count > 0 when queue is full")
	}
}

func TestStatsFiveEndpointsAndInvalidKeyDrilldown(t *testing.T) {
	st := newFakeStore()
	_, userTok := st.addUser("alice")
	cid, clientTok := st.addClientToken("bob", "macbook", store.ClientStatusActive)
	srv := New(st)

	// 正常调用 + 401 无效 token + 403 blocked client
	doRequest(t, srv, http.MethodGet, "/v1/vaults/main/metadata", userTok, nil)
	doRequest(t, srv, http.MethodGet, "/v1/vaults/main/metadata", clientTok, nil)
	doRequest(t, srv, http.MethodGet, "/v1/vaults/main/metadata", "invalid-token-1", nil)
	st.SetClientStatus(context.Background(), -1, "macbook", store.ClientStatusBlocked)
	doRequest(t, srv, http.MethodGet, "/v1/vaults/main/metadata", clientTok, nil)

	// 1. overview
	ovRec := doLANRequest(t, srv, http.MethodGet, "/stats/api/overview?range=24h", "127.0.0.1:1", "localhost", nil)
	if ovRec.Code != http.StatusOK {
		t.Fatalf("overview = %d: %s", ovRec.Code, ovRec.Body.String())
	}
	var ov store.APIStatsOverview
	if err := json.Unmarshal(ovRec.Body.Bytes(), &ov); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	if !ov.Capabilities.HasAPIKeys || ov.Capabilities.Scope != "global" || ov.Summary.TotalRequests != 4 {
		t.Fatalf("unexpected overview: %+v", ov)
	}

	// 2. routes
	rtRec := doLANRequest(t, srv, http.MethodGet, "/stats/api/routes?sort=error_rate&limit=10", "127.0.0.1:1", "localhost", nil)
	if rtRec.Code != http.StatusOK {
		t.Fatalf("routes = %d", rtRec.Code)
	}
	var rts store.APIStatsRoutesResponse
	_ = json.Unmarshal(rtRec.Body.Bytes(), &rts)
	if len(rts.Items) == 0 || rts.Items[0].RouteTemplate != "/v1/vaults/{vault}/metadata" {
		t.Fatalf("unexpected routes: %+v", rts)
	}

	// 3. errors
	erRec := doLANRequest(t, srv, http.MethodGet, "/stats/api/errors", "127.0.0.1:1", "localhost", nil)
	if erRec.Code != http.StatusOK {
		t.Fatalf("errors = %d", erRec.Code)
	}
	var errs store.APIStatsErrorsResponse
	_ = json.Unmarshal(erRec.Body.Bytes(), &errs)
	if len(errs.Items) == 0 {
		t.Fatalf("expected error items")
	}

	// 4. api-keys
	akRec := doLANRequest(t, srv, http.MethodGet, "/stats/api/api-keys", "127.0.0.1:1", "localhost", nil)
	if akRec.Code != http.StatusOK {
		t.Fatalf("api-keys = %d", akRec.Code)
	}
	var ak store.APIStatsKeysResponse
	_ = json.Unmarshal(akRec.Body.Bytes(), &ak)
	if ak.Summary.Unauthorized401Count != 1 || ak.Summary.Forbidden403Count != 1 || ak.Summary.TotalKeyRequests != 3 {
		t.Fatalf("unexpected api-keys summary: %+v", ak.Summary)
	}

	// 5. api-keys/:keyId (单 client 与 __invalid__)
	cKey := "client:" + string(rune('0'+cid))
	if cid >= 10 {
		t.Fatalf("unexpected cid %d", cid)
	}
	detRec := doLANRequest(t, srv, http.MethodGet, "/stats/api/api-keys/"+cKey, "127.0.0.1:1", "localhost", nil)
	if detRec.Code != http.StatusOK {
		t.Fatalf("api-key detail = %d", detRec.Code)
	}
	invRec := doLANRequest(t, srv, http.MethodGet, "/stats/api/api-keys/__invalid__", "127.0.0.1:1", "localhost", nil)
	if invRec.Code != http.StatusOK {
		t.Fatalf("__invalid__ detail = %d", invRec.Code)
	}
	var inv store.APIStatsKeyDetailResponse
	_ = json.Unmarshal(invRec.Body.Bytes(), &inv)
	if inv.Summary.TotalRequests != 1 {
		t.Fatalf("expected 1 invalid attempt, got %+v", inv.Summary)
	}

	// 参数校验 400
	badRec := doLANRequest(t, srv, http.MethodGet, "/stats/api/overview?range=99d", "127.0.0.1:1", "localhost", nil)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid range expected 400, got %d", badRec.Code)
	}
}

func TestStatsTTLPrune30Days(t *testing.T) {
	st := newFakeStore()
	now := time.Now().UTC()
	_ = st.RecordAccess(context.Background(), store.AccessEvent{Time: now.Add(-31 * 24 * time.Hour), Method: "GET", Path: "/old", Outcome: store.AccessOutcomeOK})
	_ = st.RecordAccess(context.Background(), store.AccessEvent{Time: now.Add(-29 * 24 * time.Hour), Method: "GET", Path: "/keep", Outcome: store.AccessOutcomeOK})

	deleted, err := st.PruneAccessLogs(context.Background(), now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("PruneAccessLogs: %v", err)
	}
	if deleted != 1 || len(st.access) != 1 || st.access[0].Path != "/keep" {
		t.Fatalf("unexpected prune result: deleted=%d remaining=%+v", deleted, st.access)
	}
}
