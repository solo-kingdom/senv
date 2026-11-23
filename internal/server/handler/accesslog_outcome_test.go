package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
)

// 结果映射：4xx 记 REJECTED、5xx 记 ERROR，认证类保持原分类。
func TestOutcomeFromStatus(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusOK, store.AccessOutcomeOK},
		{http.StatusCreated, store.AccessOutcomeOK},
		{http.StatusUnauthorized, store.AccessOutcomeAuthFailed},
		{http.StatusForbidden, store.AccessOutcomeBlocked},
		{http.StatusTooManyRequests, store.AccessOutcomeRateLimited},
		{http.StatusBadRequest, store.AccessOutcomeRejected},
		{http.StatusConflict, store.AccessOutcomeRejected},
		{http.StatusRequestEntityTooLarge, store.AccessOutcomeRejected},
		{http.StatusNotFound, store.AccessOutcomeOK},
		{http.StatusInternalServerError, store.AccessOutcomeError},
		{http.StatusServiceUnavailable, store.AccessOutcomeError},
	}
	for _, c := range cases {
		if got := outcomeFromStatus(c.status); got != c.want {
			t.Errorf("outcomeFromStatus(%d) = %q, want %q", c.status, got, c.want)
		}
	}
}

// 访问日志：新结果可入库、按 IP 过滤、push 规模（n_entries/n_deleted）保留。
func TestAccessLogRejectedAndIPFilter(t *testing.T) {
	st := store.New(testdb.New(t))
	ctx := context.Background()
	now := time.Now()

	events := []store.AccessEvent{
		{Time: now, IP: "203.0.113.9", Method: "POST", Path: "/v1/vaults/main/entries",
			Outcome: store.AccessOutcomeRejected, Reason: "revision conflict", NEntries: 3, NDeleted: 2},
		{Time: now, IP: "198.51.100.7", Method: "GET", Path: "/v1/vaults/main/entries",
			Outcome: store.AccessOutcomeOK},
		{Time: now, IP: "203.0.113.9", Method: "GET", Path: "/v1/vaults/main/entries",
			Outcome: store.AccessOutcomeError, Reason: "internal"},
	}
	for _, e := range events {
		if err := st.RecordAccess(ctx, e); err != nil {
			t.Fatalf("RecordAccess(%+v): %v", e, err)
		}
	}
	if err := st.RecordAccess(ctx, store.AccessEvent{Outcome: "BOGUS"}); err == nil {
		t.Error("unknown outcome should be rejected")
	}

	rows, err := st.ListAccessLogs(ctx, store.AccessLogFilter{IP: "203.0.113.9", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows for 203.0.113.9 = %d, want 2", len(rows))
	}
	foundPush := false
	for _, r := range rows {
		if r.Outcome == store.AccessOutcomeRejected {
			foundPush = true
			if r.NEntries != 3 || r.NDeleted != 2 {
				t.Errorf("push counts = %d/%d, want 3/2", r.NEntries, r.NDeleted)
			}
		}
		if r.Outcome == store.AccessOutcomeError && r.Reason != "internal" {
			t.Errorf("error reason = %q, want internal", r.Reason)
		}
	}
	if !foundPush {
		t.Error("rejected push event missing")
	}
}
