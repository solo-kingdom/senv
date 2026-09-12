package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
)

// newClientFixture 创建用户、注册一个 client，返回 (store, clientToken, client)
func newClientFixture(t *testing.T, userName, clientName string) (store.Store, string, *store.Client) {
	t.Helper()
	pool := testdb.New(t)
	st := store.New(pool)
	ctx := context.Background()
	userID := createUser(t, st, pool, userName)
	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	token, client, err := st.RegisterClient(ctx, code, clientName)
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	return st, token, client
}

// createUser 建用户并回查 id（handler 测试无法复用 store 包内的测试 helper）
func createUser(t *testing.T, st store.Store, pool interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, name string) int64 {
	t.Helper()
	if _, err := st.CreateUser(context.Background(), name); err != nil {
		t.Fatalf("CreateUser %s: %v", name, err)
	}
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatalf("lookup user %s: %v", name, err)
	}
	return id
}

func TestBlockedClientRejectedWith403(t *testing.T) {
	st, token, _ := newClientFixture(t, "alice", "laptop")
	srv := New(st)
	ctx := context.Background()

	if err := st.SetClientStatus(ctx, -1, "laptop", store.ClientStatusBlocked); err != nil {
		t.Fatalf("SetClientStatus: %v", err)
	}

	rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("blocked client = %d, want 403", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "client_blocked" {
		t.Errorf("blocked body = %q, want client_blocked", body["error"])
	}

	// 未知 token 仍为 401，且响应与屏蔽语义可区分
	bogus := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
	if bogus.Code != http.StatusUnauthorized {
		t.Errorf("unknown token = %d, want 401", bogus.Code)
	}
	if strings.Contains(bogus.Body.String(), "client_blocked") {
		t.Errorf("unknown token response must not leak blocked semantics: %q", bogus.Body.String())
	}
}

func TestUnblockClientRestoresAccess(t *testing.T) {
	st, token, _ := newClientFixture(t, "alice", "laptop")
	srv := New(st)
	ctx := context.Background()

	if err := st.SetClientStatus(ctx, -1, "laptop", store.ClientStatusBlocked); err != nil {
		t.Fatalf("block: %v", err)
	}
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("blocked = %d, want 403", rec.Code)
	}
	if err := st.SetClientStatus(ctx, -1, "laptop", store.ClientStatusActive); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	// 解封后原凭证恢复有效（vault 不存在 → 404）
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("after unblock = %d, want 404 (valid token, missing vault)", rec.Code)
	}
}

func TestRegisterEndpoint(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	ctx := context.Background()
	userID := createUser(t, st, pool, "alice")
	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	srv := New(st)

	rec := doRequest(t, srv, "POST", "/v1/register", "", registerRequest{Code: code, Name: "my-laptop"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", rec.Code, rec.Body.String())
	}
	var resp registerResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Token == "" || resp.Client.Name != "my-laptop" {
		t.Fatalf("register response incomplete: %+v", resp)
	}

	// 注册得到的 token 可正常认证
	if rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", resp.Token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("registered token = %d, want 404 (valid, missing vault)", rec.Code)
	}

	// 重放注册码 → 400 通用消息
	replay := doRequest(t, srv, "POST", "/v1/register", "", registerRequest{Code: code, Name: "other"})
	if replay.Code != http.StatusBadRequest || !strings.Contains(replay.Body.String(), "注册码无效") {
		t.Errorf("replay = %d %q, want 400 注册码无效", replay.Code, replay.Body.String())
	}
}

func TestRegisterEndpointErrors(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	ctx := context.Background()
	userID := createUser(t, st, pool, "alice")
	srv := New(st)

	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	if _, _, err := st.RegisterClient(ctx, code, "laptop"); err != nil {
		t.Fatalf("seed register: %v", err)
	}
	code2, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}

	// 同名冲突 → 409
	rec := doRequest(t, srv, "POST", "/v1/register", "", registerRequest{Code: code2, Name: "laptop"})
	if rec.Code != http.StatusConflict {
		t.Errorf("name conflict = %d, want 409", rec.Code)
	}

	// 缺 name → 400
	rec = doRequest(t, srv, "POST", "/v1/register", "", registerRequest{Code: code2, Name: "  "})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty name = %d, want 400", rec.Code)
	}
}

func TestRegisterRateLimited(t *testing.T) {
	st := store.New(testdb.New(t))
	srv := New(st, Options{AuthRateLimit: 2})
	for i := 0; i < 2; i++ {
		rec := doRequest(t, srv, "POST", "/v1/register", "", registerRequest{Code: "bogus", Name: "dev"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid code #%d = %d, want 400", i, rec.Code)
		}
	}
	rec := doRequest(t, srv, "POST", "/v1/register", "", registerRequest{Code: "bogus", Name: "dev"})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("after %d failures = %d, want 429", 2, rec.Code)
	}
}
