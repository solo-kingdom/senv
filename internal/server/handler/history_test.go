package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
)

// historyFixture 返回 server、alice 的 client token，并造出 deploy:KEY 的
// 三次推送（两次修改 → 两条历史）。
func historyFixture(t *testing.T) (store.Store, *Server, string) {
	t.Helper()
	pool := testdb.New(t)
	st := store.New(pool)
	ctx := context.Background()
	userID := createUser(t, st, pool, "alice")

	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	clientToken, _, err := st.RegisterClient(ctx, code, "dev")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	push := func(base int64, val string) {
		if _, _, err := st.PushEntries(ctx, userID, "main", []store.Entry{
			{Kind: "env", Grp: "deploy", Key: "KEY", BaseRevision: base, Ciphertext: []byte(val)},
		}); err != nil {
			t.Fatalf("push: %v", err)
		}
	}
	push(0, "v1")
	push(1, "v2")
	push(2, "v3")
	return st, New(st), clientToken
}

func TestHistoryEndpointRequiresAuth(t *testing.T) {
	st, srv, _ := historyFixture(t)
	_ = st
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/history", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", rec.Code)
	}
}

func TestHistoryEndpointVaultIsolation(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	viewerTok, err := st.CreateUser(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	srv := New(st)
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/history", viewerTok, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user history = %d, want 404", rec.Code)
	}
}

func TestHistoryEndpointFilterAndOrder(t *testing.T) {
	st, srv, clientToken := historyFixture(t)
	_ = st

	// 条目历史：revision 新到旧
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/history?kind=env&grp=deploy&key=KEY", clientToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		History []store.HistoryVersion `json:"history"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.History) != 2 || resp.History[0].Revision != 2 || string(resp.History[0].Ciphertext) != "v2" {
		t.Errorf("entry history = %+v, want [rev2 v2, rev1 v1]", resp.History)
	}

	// vault 级最近（无过滤）
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/history", clientToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("recent history = %d", rec.Code)
	}
	resp.History = nil
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.History) != 2 {
		t.Errorf("recent history = %d rows, want 2", len(resp.History))
	}

	// 非法 limit → 400
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/history?limit=abc", clientToken, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad limit = %d, want 400", rec.Code)
	}
}
