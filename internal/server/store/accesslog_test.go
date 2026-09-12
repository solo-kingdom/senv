package store

import (
	"context"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/testdb"
)

func recordEvent(t *testing.T, s *pgStore, e AccessEvent) {
	t.Helper()
	if err := s.RecordAccess(context.Background(), e); err != nil {
		t.Fatalf("RecordAccess: %v", err)
	}
}

func TestAccessLogRecordAndFilter(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	now := time.Now()
	recordEvent(t, s, AccessEvent{Time: now.Add(-2 * time.Hour), IP: "10.0.0.1", Method: "GET", Path: "/v1/vaults/main/entries", UserID: userID, Outcome: AccessOutcomeOK})
	recordEvent(t, s, AccessEvent{Time: now.Add(-1 * time.Hour), IP: "10.0.0.2", Method: "GET", Path: "/v1/vaults/main/entries", Outcome: AccessOutcomeAuthFailed, Reason: "invalid or revoked token"})
	recordEvent(t, s, AccessEvent{Time: now, IP: "10.0.0.1", Method: "GET", Path: "/v1/vaults/main/entries", UserID: userID, Outcome: AccessOutcomeBlocked, Reason: "client blocked"})

	// 全量，时间新到旧
	all, err := s.ListAccessLogs(ctx, AccessLogFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %d, %v; want 3", len(all), err)
	}
	if all[0].Outcome != AccessOutcomeBlocked {
		t.Errorf("newest = %s, want BLOCKED", all[0].Outcome)
	}
	if all[0].UserName != "alice" {
		t.Errorf("user name join = %q, want alice", all[0].UserName)
	}

	// 结果过滤
	failed, _ := s.ListAccessLogs(ctx, AccessLogFilter{Outcome: AccessOutcomeAuthFailed})
	if len(failed) != 1 || failed[0].Reason != "invalid or revoked token" {
		t.Errorf("outcome filter = %+v", failed)
	}

	// 用户过滤
	mine, _ := s.ListAccessLogs(ctx, AccessLogFilter{User: &userID})
	if len(mine) != 2 {
		t.Errorf("user filter = %d, want 2", len(mine))
	}

	// 时间过滤：只看最近 90 分钟
	since := now.Add(-90 * time.Minute)
	recent, _ := s.ListAccessLogs(ctx, AccessLogFilter{Since: &since})
	if len(recent) != 2 {
		t.Errorf("since filter = %d, want 2", len(recent))
	}

	// limit
	limited, _ := s.ListAccessLogs(ctx, AccessLogFilter{Limit: 1})
	if len(limited) != 1 {
		t.Errorf("limit = %d, want 1", len(limited))
	}
}

func TestAccessLogPrune(t *testing.T) {
	s := NewSQL(testdb.New(t))
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 5; i++ {
		recordEvent(t, s, AccessEvent{Time: now.Add(-time.Duration(5-i) * time.Hour), IP: "10.0.0.1", Method: "GET", Path: "/x", Outcome: AccessOutcomeOK})
	}
	// 删除 2.5 小时前（含 3 条更早的）
	n, err := s.PruneAccessLogs(ctx, now.Add(-2*time.Hour-30*time.Minute))
	if err != nil {
		t.Fatalf("PruneAccessLogs: %v", err)
	}
	if n != 3 {
		t.Fatalf("pruned = %d, want 3", n)
	}
	rest, _ := s.ListAccessLogs(ctx, AccessLogFilter{})
	if len(rest) != 2 {
		t.Errorf("remaining = %d, want 2", len(rest))
	}
	// 再删一次：无匹配
	n, err = s.PruneAccessLogs(ctx, now.Add(-2*time.Hour-30*time.Minute))
	if err != nil || n != 0 {
		t.Errorf("second prune = %d, %v; want 0", n, err)
	}
}

func TestAccessLogZeroTimeDefaultsToNow(t *testing.T) {
	s := NewSQL(testdb.New(t))
	recordEvent(t, s, AccessEvent{IP: "10.0.0.9", Method: "GET", Path: "/x", Outcome: AccessOutcomeOK})
	events, err := s.ListAccessLogs(context.Background(), AccessLogFilter{})
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %d, %v; want 1", len(events), err)
	}
	if age := time.Since(events[0].Time); age > time.Minute || age < -time.Minute {
		t.Errorf("event time should default to now, got %v (age %v)", events[0].Time, age)
	}
}
