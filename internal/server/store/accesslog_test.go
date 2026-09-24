package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func TestAccessLogAdminOutcome(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	s := NewSQL(pool)

	userID := mustUserID(t, ctx, pool, "audit-admin")
	recordEvent(t, s, AccessEvent{IP: "-", Method: "ADMIN", Path: "admin",
		Outcome: AccessOutcomeAdmin, Reason: "create-user audit-admin", UserID: userID})

	// 非法 outcome 仍被拒绝（ADMIN 之外的未知值）
	if err := s.RecordAccess(ctx, AccessEvent{IP: "10.0.0.1", Method: "GET", Path: "/x", Outcome: "BOGUS"}); err == nil {
		t.Fatal("期望非法 outcome 被拒绝")
	}

	// --outcome ADMIN 过滤可查
	events, err := s.ListAccessLogs(ctx, AccessLogFilter{Outcome: AccessOutcomeAdmin})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ADMIN 事件数 = %d, want 1", len(events))
	}
	if events[0].Reason != "create-user audit-admin" || events[0].UserName != "audit-admin" {
		t.Errorf("事件内容不符: %+v", events[0])
	}
}

// mustUserID 建用户并返回 id（审计测试用）
func mustUserID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}
