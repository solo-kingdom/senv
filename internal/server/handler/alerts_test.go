// 告警检测器测试：webhook 用 httptest 收集 payload，异步投递靠轮询等待。
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/store"
)

// alertRecorder 收集 webhook 收到的告警
type alertRecorder struct {
	mu   sync.Mutex
	body []alertPayload
}

func (r *alertRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var p alertPayload
	if err := json.NewDecoder(req.Body).Decode(&p); err == nil {
		r.mu.Lock()
		r.body = append(r.body, p)
		r.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (r *alertRecorder) alertsOf(typ string) []alertPayload {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []alertPayload{}
	for _, p := range r.body {
		if p.Alert == typ {
			out = append(out, p)
		}
	}
	return out
}

// waitForAlerts 轮询直到出现 ≥want 条指定类型告警（异步投递，最多等 3s）
func waitForAlerts(t *testing.T, r *alertRecorder, typ string, want int) []alertPayload {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := r.alertsOf(typ); len(got) >= want {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 %s 告警 x%d 超时，已收到: %v", typ, want, r.alertsOf(typ))
	return nil
}

func TestAlertAuthFailStormAndDebounce(t *testing.T) {
	rec := &alertRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	st := newFakeStore()
	s := New(st, Options{AlertWebhook: srv.URL, AlertAuthFailThreshold: 3, AlertDebounce: time.Hour})

	// 3 次无效 token → 触发一次 auth_fail_storm
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/v1/vaults/main/entries", nil)
		req.Header.Set("Authorization", "Bearer bogus")
		s.ServeHTTP(httptest.NewRecorder(), req)
	}
	got := waitForAlerts(t, rec, "auth_fail_storm", 1)
	if got[0].IP == "" || got[0].Count < 3 {
		t.Errorf("storm payload 不符: %+v", got[0])
	}

	// 阈值下不触发：新 server 同 recorder，2 次失败
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/v1/vaults/main/entries", nil)
		req.Header.Set("Authorization", "Bearer bogus")
		s.ServeHTTP(httptest.NewRecorder(), req)
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(rec.alertsOf("auth_fail_storm")); n != 1 {
		t.Fatalf("去抖窗口内重复告警: got %d, want 1", n)
	}
}

func TestAlertClientBlocked(t *testing.T) {
	rec := &alertRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	st := newFakeStore()
	st.addUser("alice")
	_, token := st.addClientToken("alice", "laptop", store.ClientStatusBlocked)
	s := New(st, Options{AlertWebhook: srv.URL})

	req := httptest.NewRequest("GET", "/v1/vaults/main/entries", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.ServeHTTP(httptest.NewRecorder(), req)

	got := waitForAlerts(t, rec, "client_blocked", 1)
	if got[0].ClientID == 0 {
		t.Errorf("blocked payload 缺 client id: %+v", got[0])
	}
}

func TestAlertClientNewIP(t *testing.T) {
	rec := &alertRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	st := newFakeStore()
	st.addClientToken("alice", "laptop", store.ClientStatusActive)
	_, token := st.addClientToken("alice", "laptop2", store.ClientStatusActive)
	s := New(st, Options{AlertWebhook: srv.URL, TrustProxyHeaders: true})

	get := func(ip string) {
		req := httptest.NewRequest("GET", "/v1/vaults/main/metadata", nil)
		req.RemoteAddr = "127.0.0.1:1234" // loopback 对端才采信 X-Real-IP
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Real-IP", ip)
		s.ServeHTTP(httptest.NewRecorder(), req)
	}
	get("1.1.1.1") // 首次：建立 lastIP，不告警
	time.Sleep(200 * time.Millisecond)
	if n := len(rec.alertsOf("client_new_ip")); n != 0 {
		t.Fatalf("首次访问不应报 new_ip: got %d", n)
	}
	get("2.2.2.2") // 换 IP → 告警
	got := waitForAlerts(t, rec, "client_new_ip", 1)
	if got[0].IP != "2.2.2.2" {
		t.Errorf("new_ip payload IP = %q, want 2.2.2.2", got[0].IP)
	}
	// 同一新 IP 再去抖窗口内不重复
	get("1.1.1.1")
	time.Sleep(200 * time.Millisecond)
	if n := len(rec.alertsOf("client_new_ip")); n != 1 {
		t.Fatalf("new_ip 去抖失效: got %d, want 1", n)
	}
}

func TestAlertClientRegistered(t *testing.T) {
	rec := &alertRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	st := newFakeStore()
	uid, _ := st.addUser("alice")
	code, _ := st.CreateRegistrationCode(nil, uid, time.Minute)
	s := New(st, Options{AlertWebhook: srv.URL})

	body := fmt.Sprintf(`{"code":%q,"name":"laptop"}`, code)
	req := httptest.NewRequest("POST", "/v1/register", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register 状态码 = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}

	got := waitForAlerts(t, rec, "client_registered", 1)
	if got[0].ClientID == 0 {
		t.Errorf("registered payload 缺 client id: %+v", got[0])
	}
}
