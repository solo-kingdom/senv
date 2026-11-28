package store

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/testdb"
)

func TestPGStatsQueriesAndPercentiles(t *testing.T) {
	pool := testdb.New(t)
	st := NewSQL(pool)
	ctx := context.Background()

	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (name, token_hash) VALUES ('stats-user', 'hash-stats') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	_, client, err := st.RegisterClient(ctx, code, "stats-laptop")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	now := time.Now().UTC()
	samples := []AccessEvent{
		{Time: now.Add(-10 * time.Minute), IP: "10.0.0.1", Method: "GET", Path: "/v1/vaults/main/entries", RouteTemplate: "/v1/vaults/{vault}/entries", StatusCode: 200, DurationMs: 10, AuthType: "api_key", ClientID: client.ID, UserID: userID, Outcome: AccessOutcomeOK},
		{Time: now.Add(-9 * time.Minute), IP: "10.0.0.1", Method: "GET", Path: "/v1/vaults/main/entries", RouteTemplate: "/v1/vaults/{vault}/entries", StatusCode: 200, DurationMs: 50, AuthType: "api_key", ClientID: client.ID, UserID: userID, Outcome: AccessOutcomeOK},
		{Time: now.Add(-8 * time.Minute), IP: "10.0.0.1", Method: "GET", Path: "/v1/vaults/main/entries", RouteTemplate: "/v1/vaults/{vault}/entries", StatusCode: 200, DurationMs: 100, AuthType: "api_key", ClientID: client.ID, UserID: userID, Outcome: AccessOutcomeOK},
		{Time: now.Add(-7 * time.Minute), IP: "10.0.0.2", Method: "GET", Path: "/v1/vaults/main/entries", RouteTemplate: "/v1/vaults/{vault}/entries", StatusCode: 401, DurationMs: 5, AuthType: "invalid_key", Outcome: AccessOutcomeAuthFailed, Reason: "invalid token"},
		{Time: now.Add(-6 * time.Minute), IP: "10.0.0.3", Method: "GET", Path: "/unknown", RouteTemplate: "/_unmatched", StatusCode: 404, DurationMs: 3, AuthType: "anonymous", Outcome: AccessOutcomeOK},
		{Time: now.Add(-5 * time.Minute), IP: "admin", Method: "ADMIN", Path: "create-user", Outcome: AccessOutcomeAdmin, Reason: "create-user"},
	}
	for _, ev := range samples {
		if err := st.RecordAccess(ctx, ev); err != nil {
			t.Fatalf("RecordAccess: %v", err)
		}
	}

	ov, err := st.StatsOverview(ctx, APIStatsQuery{Range: "24h"})
	if err != nil {
		t.Fatalf("StatsOverview: %v", err)
	}
	if ov.Summary.TotalRequests != 5 {
		t.Fatalf("total_requests = %d, want 5 (ADMIN excluded)", ov.Summary.TotalRequests)
	}
	if math.Abs(ov.Summary.P50Ms-10) > 0.01 {
		t.Fatalf("p50_ms = %v, want 10", ov.Summary.P50Ms)
	}

	rts, err := st.StatsRoutes(ctx, APIStatsQuery{Range: "24h", Sort: "count"})
	if err != nil {
		t.Fatalf("StatsRoutes: %v", err)
	}
	if len(rts.Items) != 2 || rts.Items[0].RouteTemplate != "/v1/vaults/{vault}/entries" {
		t.Fatalf("unexpected routes: %+v", rts.Items)
	}

	errs, err := st.StatsErrors(ctx, APIStatsQuery{Range: "24h"})
	if err != nil {
		t.Fatalf("StatsErrors: %v", err)
	}
	if len(errs.Items) != 2 {
		t.Fatalf("errors count = %d, want 2", len(errs.Items))
	}

	ak, err := st.StatsAPIKeys(ctx, APIStatsQuery{Range: "24h"})
	if err != nil {
		t.Fatalf("StatsAPIKeys: %v", err)
	}
	if ak.Summary.TotalKeyRequests != 3 || ak.Summary.Unauthorized401Count != 1 {
		t.Fatalf("unexpected api-keys summary: %+v", ak.Summary)
	}

	inv, err := st.StatsAPIKeyDetail(ctx, "__invalid__", APIStatsQuery{Range: "24h"})
	if err != nil {
		t.Fatalf("StatsAPIKeyDetail(__invalid__): %v", err)
	}
	if inv.Summary.TotalRequests != 1 {
		t.Fatalf("__invalid__ total = %d, want 1", inv.Summary.TotalRequests)
	}
}
