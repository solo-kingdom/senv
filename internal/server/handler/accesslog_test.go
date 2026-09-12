package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
)

// listEvents 读取全部访问日志
func listEvents(t *testing.T, st store.Store) []store.AccessEventRow {
	t.Helper()
	events, err := st.ListAccessLogs(context.Background(), store.AccessLogFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListAccessLogs: %v", err)
	}
	return events
}

func TestAccessLogOutcomes(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	ctx := context.Background()
	userID := createUser(t, st, pool, "alice")

	// 注册一个 client 用于 BLOCKED 场景
	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	clientToken, _, err := st.RegisterClient(ctx, code, "laptop")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	// alice 的 user 级 token 用于 OK 场景
	userToken, err := st.CreateUser(ctx, "alice-legacy")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = userID

	srv := New(st)
	// OK：有效凭证
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", userToken, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("valid token = %d, want 404", rec.Code)
	}
	// AUTH-FAILED：坏 token
	doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
	// BLOCKED：屏蔽后请求
	if err := st.SetClientStatus(ctx, -1, "laptop", store.ClientStatusBlocked); err != nil {
		t.Fatalf("block: %v", err)
	}
	doRequest(t, srv, "GET", "/v1/vaults/main/metadata", clientToken, nil)
	// RATE-LIMITED：关闭全局限速影响，用独立 server 构造
	limitedSrv := New(st, Options{AuthRateLimit: 1})
	doRequest(t, limitedSrv, "GET", "/v1/vaults/main/metadata", "bad1", nil)
	doRequest(t, limitedSrv, "GET", "/v1/vaults/main/metadata", "bad2", nil)

	events := listEvents(t, st)
	outcomes := map[string]string{}
	for _, e := range events {
		outcomes[e.Outcome] = e.Reason
	}
	for _, want := range []string{store.AccessOutcomeOK, store.AccessOutcomeAuthFailed, store.AccessOutcomeBlocked, store.AccessOutcomeRateLimited} {
		if _, ok := outcomes[want]; !ok {
			t.Errorf("outcome %s missing, got %v", want, outcomes)
		}
	}

	// BLOCKED 事件应带 client 身份与原因
	blocked, _ := st.ListAccessLogs(ctx, store.AccessLogFilter{Outcome: store.AccessOutcomeBlocked})
	if len(blocked) != 1 || blocked[0].ClientName != "laptop" || blocked[0].Reason != "client blocked" {
		t.Errorf("blocked event = %+v", blocked)
	}
	// AUTH-FAILED 不泄露身份（user=0）
	failed, _ := st.ListAccessLogs(ctx, store.AccessLogFilter{Outcome: store.AccessOutcomeAuthFailed})
	for _, e := range failed {
		if e.UserID != 0 || e.ClientID != 0 {
			t.Errorf("auth-failed event must not carry identity: %+v", e)
		}
	}
}

func TestAccessLogHealthzSkipped(t *testing.T) {
	st := store.New(testdb.New(t))
	srv := New(st)
	doRequest(t, srv, "GET", "/healthz", "", nil)
	if events := listEvents(t, st); len(events) != 0 {
		t.Errorf("healthz must not be logged, got %d events", len(events))
	}
}

func TestAccessLogNoSecretLeakage(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	secretToken, err := st.CreateUser(context.Background(), "alice")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	srv := New(st)
	doRequest(t, srv, "GET", "/v1/vaults/main/metadata", secretToken, nil)
	doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus-token-value", nil)

	// 事件全字段拼接后不得包含任何 token 明文
	for _, e := range listEvents(t, st) {
		joined := e.IP + e.Method + e.Path + e.Reason + e.Outcome
		if strings.Contains(joined, secretToken) || strings.Contains(joined, "bogus-token-value") {
			t.Errorf("access log leaks token material: %+v", e)
		}
	}
}
