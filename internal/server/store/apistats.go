package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// APIStatsQuery 描述接口访问统计的通用过滤与排序参数。
type APIStatsQuery struct {
	Range       string
	From        time.Time
	To          time.Time
	Method      string
	Route       string
	StatusClass string
	AuthType    string
	ApiKeyID    string
	Sort        string
	Limit       int
}

// ResolveWindow 返回归一化后的 [from, to] 时间窗口和步长标识（minute/hour/day）。
func (q APIStatsQuery) ResolveWindow(now time.Time) (time.Time, time.Time, string) {
	to := q.To
	if to.IsZero() {
		to = now.UTC()
	} else {
		to = to.UTC()
	}
	from := q.From
	if from.IsZero() {
		var dur time.Duration
		switch q.Range {
		case "1h":
			dur = time.Hour
		case "7d":
			dur = 7 * 24 * time.Hour
		case "30d":
			dur = 30 * 24 * time.Hour
		default:
			dur = 24 * time.Hour
		}
		from = to.Add(-dur)
	} else {
		from = from.UTC()
	}
	if from.After(to) {
		from = to.Add(-24 * time.Hour)
	}
	span := to.Sub(from)
	granularity := "hour"
	switch {
	case span <= 2*time.Hour:
		granularity = "minute"
	case span > 48*time.Hour:
		granularity = "day"
	}
	return from, to, granularity
}

type APIStatsCapabilities struct {
	HasAPIKeys bool   `json:"has_api_keys"`
	Scope      string `json:"scope"`
}

type APIStatsSummary struct {
	TotalRequests int64   `json:"total_requests"`
	RPS           float64 `json:"rps"`
	SuccessRate   float64 `json:"success_rate"`
	Error4xxRate  float64 `json:"error_4xx_rate"`
	Error5xxRate  float64 `json:"error_5xx_rate"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	P50Ms         float64 `json:"p50_ms"`
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
	ActiveRoutes  int64   `json:"active_routes"`
}

type APIStatsSeriesPoint struct {
	Bucket   time.Time `json:"bucket"`
	Requests int64     `json:"requests"`
	Error4xx int64     `json:"error_4xx"`
	Error5xx int64     `json:"error_5xx"`
	P95Ms    float64   `json:"p95_ms"`
}

type APIStatsStatusCount struct {
	StatusClass string `json:"status_class"`
	Count       int64  `json:"count"`
}

type APIStatsAuthSourceCount struct {
	AuthType string `json:"auth_type"`
	Count    int64  `json:"count"`
}

type APIStatsOverview struct {
	Capabilities       APIStatsCapabilities      `json:"capabilities"`
	Summary            APIStatsSummary           `json:"summary"`
	Series             []APIStatsSeriesPoint     `json:"series"`
	StatusDistribution []APIStatsStatusCount     `json:"status_distribution"`
	AuthSources        []APIStatsAuthSourceCount `json:"auth_sources"`
}

type APIStatsRouteItem struct {
	Method        string  `json:"method"`
	RouteTemplate string  `json:"route_template"`
	Count         int64   `json:"count"`
	Error4xx      int64   `json:"error_4xx"`
	Error5xx      int64   `json:"error_5xx"`
	ErrorRate     float64 `json:"error_rate"`
	AvgMs         float64 `json:"avg_ms"`
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
}

type APIStatsRoutesResponse struct {
	Items []APIStatsRouteItem `json:"items"`
}

type APIStatsErrorItem struct {
	OccurredAt    time.Time `json:"occurred_at"`
	Method        string    `json:"method"`
	RouteTemplate string    `json:"route_template"`
	StatusCode    int       `json:"status_code"`
	DurationMs    int       `json:"duration_ms"`
	AuthType      string    `json:"auth_type"`
	APIKeyID      string    `json:"api_key_id,omitempty"`
	APIKeyPrefix  string    `json:"api_key_prefix,omitempty"`
	APIKeyName    string    `json:"api_key_name,omitempty"`
	ClientIP      string    `json:"client_ip,omitempty"`
	ErrorCode     string    `json:"error_code,omitempty"`
}

type APIStatsErrorsResponse struct {
	Items []APIStatsErrorItem `json:"items"`
}

type APIStatsKeysSummary struct {
	TotalKeyRequests     int64 `json:"total_key_requests"`
	ActiveKeyCount       int64 `json:"active_key_count"`
	Unauthorized401Count int64 `json:"unauthorized_401_count"`
	Forbidden403Count    int64 `json:"forbidden_403_count"`
}

type APIStatsKeyItem struct {
	APIKeyID     string     `json:"api_key_id"`
	APIKeyPrefix string     `json:"api_key_prefix"`
	APIKeyName   string     `json:"api_key_name"`
	UserID       string     `json:"user_id,omitempty"`
	UserName     string     `json:"user_name,omitempty"`
	Status       string     `json:"status"`
	Count        int64      `json:"count"`
	ShareRate    float64    `json:"share_rate"`
	Error4xx     int64      `json:"error_4xx"`
	Error5xx     int64      `json:"error_5xx"`
	P95Ms        float64    `json:"p95_ms"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}

type APIStatsKeysResponse struct {
	Summary APIStatsKeysSummary `json:"summary"`
	Items   []APIStatsKeyItem   `json:"items"`
}

type APIStatsKeyDetailSummary struct {
	TotalRequests int64      `json:"total_requests"`
	Error4xx      int64      `json:"error_4xx"`
	Error5xx      int64      `json:"error_5xx"`
	P95Ms         float64    `json:"p95_ms"`
	LastActiveAt  *time.Time `json:"last_active_at,omitempty"`
}

type APIStatsKeyDetailResponse struct {
	KeyID        string                   `json:"key_id"`
	KeyPrefix    string                   `json:"key_prefix"`
	KeyName      string                   `json:"key_name"`
	Status       string                   `json:"status"`
	Summary      APIStatsKeyDetailSummary `json:"summary"`
	Series       []APIStatsSeriesPoint    `json:"series"`
	TopRoutes    []APIStatsRouteItem      `json:"top_routes"`
	ErrorRoutes  []APIStatsRouteItem      `json:"error_routes"`
	RecentErrors []APIStatsErrorItem      `json:"recent_errors"`
}

const (
	statsRouteExpr    = "COALESCE(NULLIF(l.route_template, ''), l.path)"
	statsAuthTypeExpr = "COALESCE(NULLIF(l.auth_type, ''), CASE WHEN l.client_id IS NOT NULL OR l.user_id IS NOT NULL THEN 'api_key' ELSE 'anonymous' END)"
	statsKeyIDExpr    = "CASE WHEN l.client_id IS NOT NULL THEN 'client:' || l.client_id::text WHEN l.user_id IS NOT NULL THEN 'user:' || l.user_id::text ELSE '' END"
)

func buildStatsWhere(q APIStatsQuery, from, to time.Time) (string, []any) {
	args := []any{from, to}
	conds := []string{
		"l.outcome <> 'ADMIN'",
		"l.ts >= $1",
		"l.ts <= $2",
	}
	if m := strings.ToUpper(strings.TrimSpace(q.Method)); m != "" {
		args = append(args, m)
		conds = append(conds, fmt.Sprintf("UPPER(l.method) = $%d", len(args)))
	}
	if rt := strings.TrimSpace(q.Route); rt != "" {
		args = append(args, rt)
		conds = append(conds, fmt.Sprintf("%s = $%d", statsRouteExpr, len(args)))
	}
	switch strings.ToLower(strings.TrimSpace(q.StatusClass)) {
	case "2xx":
		conds = append(conds, "l.status_code >= 200 AND l.status_code < 300")
	case "3xx":
		conds = append(conds, "l.status_code >= 300 AND l.status_code < 400")
	case "4xx":
		conds = append(conds, "l.status_code >= 400 AND l.status_code < 500")
	case "5xx":
		conds = append(conds, "l.status_code >= 500 AND l.status_code < 600")
	}
	if at := strings.TrimSpace(q.AuthType); at != "" {
		args = append(args, at)
		conds = append(conds, fmt.Sprintf("%s = $%d", statsAuthTypeExpr, len(args)))
	}
	if keyID := strings.TrimSpace(q.ApiKeyID); keyID != "" {
		conds = append(conds, buildKeyIDCondition(keyID, &args))
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func buildKeyIDCondition(keyID string, args *[]any) string {
	if keyID == "__invalid__" {
		return fmt.Sprintf("(%s = 'invalid_key' OR l.status_code = 401)", statsAuthTypeExpr)
	}
	if raw, ok := strings.CutPrefix(keyID, "client:"); ok {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return "1 = 0"
		}
		*args = append(*args, id)
		return fmt.Sprintf("l.client_id = $%d", len(*args))
	}
	if raw, ok := strings.CutPrefix(keyID, "user:"); ok {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return "1 = 0"
		}
		*args = append(*args, id)
		return fmt.Sprintf("(l.client_id IS NULL AND l.user_id = $%d)", len(*args))
	}
	id, err := strconv.ParseInt(keyID, 10, 64)
	if err != nil || id <= 0 {
		return "1 = 0"
	}
	*args = append(*args, id)
	return fmt.Sprintf("(l.client_id = $%d OR (l.client_id IS NULL AND l.user_id = $%d))", len(*args), len(*args))
}

func (s *pgStore) StatsOverview(ctx context.Context, q APIStatsQuery) (APIStatsOverview, error) {
	from, to, granularity := q.ResolveWindow(time.Now().UTC())
	where, args := buildStatsWhere(q, from, to)

	out := APIStatsOverview{
		Capabilities:       APIStatsCapabilities{HasAPIKeys: true, Scope: "global"},
		Series:             []APIStatsSeriesPoint{},
		StatusDistribution: []APIStatsStatusCount{},
		AuthSources:        []APIStatsAuthSourceCount{},
	}

	var total, okCount, err4xx, err5xx, activeRoutes int64
	var avgMs, p50, p95, p99 float64
	summarySQL := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 200 AND l.status_code < 400)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 400 AND l.status_code < 500)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 500)::bigint,
			COALESCE(AVG(l.duration_ms), 0)::float8,
			COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8,
			COUNT(DISTINCT %s)::bigint
		FROM access_log l %s`, statsRouteExpr, where)
	if err := s.pool.QueryRow(ctx, summarySQL, args...).Scan(
		&total, &okCount, &err4xx, &err5xx, &avgMs, &p50, &p95, &p99, &activeRoutes,
	); err != nil {
		return out, err
	}
	spanSec := to.Sub(from).Seconds()
	if spanSec < 1 {
		spanSec = 1
	}
	out.Summary = APIStatsSummary{
		TotalRequests: total,
		RPS:           float64(total) / spanSec,
		AvgDurationMs: avgMs,
		P50Ms:         p50,
		P95Ms:         p95,
		P99Ms:         p99,
		ActiveRoutes:  activeRoutes,
	}
	if total > 0 {
		out.Summary.SuccessRate = float64(okCount) / float64(total)
		out.Summary.Error4xxRate = float64(err4xx) / float64(total)
		out.Summary.Error5xxRate = float64(err5xx) / float64(total)
	}

	seriesSQL := fmt.Sprintf(`
		SELECT
			date_trunc('%s', l.ts AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket,
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 400 AND l.status_code < 500)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 500)::bigint,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8
		FROM access_log l %s
		GROUP BY bucket
		ORDER BY bucket ASC`, granularity, where)
	sRows, err := s.pool.Query(ctx, seriesSQL, args...)
	if err != nil {
		return out, err
	}
	for sRows.Next() {
		var pt APIStatsSeriesPoint
		if err := sRows.Scan(&pt.Bucket, &pt.Requests, &pt.Error4xx, &pt.Error5xx, &pt.P95Ms); err != nil {
			sRows.Close()
			return out, err
		}
		out.Series = append(out.Series, pt)
	}
	if err := sRows.Err(); err != nil {
		sRows.Close()
		return out, err
	}
	sRows.Close()

	distSQL := fmt.Sprintf(`
		SELECT
			CASE
				WHEN l.status_code >= 200 AND l.status_code < 300 THEN '2xx'
				WHEN l.status_code >= 300 AND l.status_code < 400 THEN '3xx'
				WHEN l.status_code >= 400 AND l.status_code < 500 THEN '4xx'
				WHEN l.status_code >= 500 THEN '5xx'
				ELSE 'other'
			END AS cls,
			COUNT(*)::bigint
		FROM access_log l %s
		GROUP BY cls
		ORDER BY cls ASC`, where)
	dRows, err := s.pool.Query(ctx, distSQL, args...)
	if err != nil {
		return out, err
	}
	for dRows.Next() {
		var item APIStatsStatusCount
		if err := dRows.Scan(&item.StatusClass, &item.Count); err != nil {
			dRows.Close()
			return out, err
		}
		out.StatusDistribution = append(out.StatusDistribution, item)
	}
	if err := dRows.Err(); err != nil {
		dRows.Close()
		return out, err
	}
	dRows.Close()

	authSQL := fmt.Sprintf(`
		SELECT %s AS src, COUNT(*)::bigint
		FROM access_log l %s
		GROUP BY src
		ORDER BY COUNT(*) DESC, src ASC`, statsAuthTypeExpr, where)
	aRows, err := s.pool.Query(ctx, authSQL, args...)
	if err != nil {
		return out, err
	}
	for aRows.Next() {
		var item APIStatsAuthSourceCount
		if err := aRows.Scan(&item.AuthType, &item.Count); err != nil {
			aRows.Close()
			return out, err
		}
		out.AuthSources = append(out.AuthSources, item)
	}
	if err := aRows.Err(); err != nil {
		aRows.Close()
		return out, err
	}
	aRows.Close()
	return out, nil
}

func (s *pgStore) StatsRoutes(ctx context.Context, q APIStatsQuery) (APIStatsRoutesResponse, error) {
	from, to, _ := q.ResolveWindow(time.Now().UTC())
	where, args := buildStatsWhere(q, from, to)
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	orderBy := "cnt DESC, p95_ms DESC"
	switch strings.ToLower(strings.TrimSpace(q.Sort)) {
	case "error_rate":
		orderBy = "error_rate DESC, cnt DESC"
	case "p95":
		orderBy = "p95_ms DESC, cnt DESC"
	}
	args = append(args, limit)
	sqlStr := fmt.Sprintf(`
		SELECT
			UPPER(l.method) AS method,
			%s AS rt,
			COUNT(*)::bigint AS cnt,
			COUNT(*) FILTER (WHERE l.status_code >= 400 AND l.status_code < 500)::bigint AS err4,
			COUNT(*) FILTER (WHERE l.status_code >= 500)::bigint AS err5,
			CASE WHEN COUNT(*) > 0
				THEN (COUNT(*) FILTER (WHERE l.status_code >= 400))::float8 / COUNT(*)::float8
				ELSE 0 END AS error_rate,
			COALESCE(AVG(l.duration_ms), 0)::float8 AS avg_ms,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8 AS p95_ms,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8 AS p99_ms
		FROM access_log l %s
		GROUP BY UPPER(l.method), rt
		ORDER BY %s
		LIMIT $%d`, statsRouteExpr, where, orderBy, len(args))

	rows, err := s.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return APIStatsRoutesResponse{}, err
	}
	defer rows.Close()

	items := []APIStatsRouteItem{}
	for rows.Next() {
		var it APIStatsRouteItem
		if err := rows.Scan(&it.Method, &it.RouteTemplate, &it.Count, &it.Error4xx, &it.Error5xx, &it.ErrorRate, &it.AvgMs, &it.P95Ms, &it.P99Ms); err != nil {
			return APIStatsRoutesResponse{}, err
		}
		items = append(items, it)
	}
	return APIStatsRoutesResponse{Items: items}, rows.Err()
}

func (s *pgStore) StatsErrors(ctx context.Context, q APIStatsQuery) (APIStatsErrorsResponse, error) {
	from, to, _ := q.ResolveWindow(time.Now().UTC())
	where, args := buildStatsWhere(q, from, to)
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	args = append(args, limit)
	sqlStr := fmt.Sprintf(`
		SELECT
			l.ts,
			UPPER(l.method),
			%s,
			l.status_code,
			l.duration_ms,
			%s,
			%s,
			CASE
				WHEN l.client_id IS NOT NULL THEN 'client#' || l.client_id::text
				WHEN l.user_id IS NOT NULL THEN 'user#' || l.user_id::text
				ELSE ''
			END,
			CASE
				WHEN l.client_id IS NOT NULL THEN COALESCE(c.name, 'client#' || l.client_id::text)
				WHEN l.user_id IS NOT NULL THEN COALESCE(u.name, 'user#' || l.user_id::text) || ' (user-token)'
				ELSE ''
			END,
			l.ip,
			COALESCE(l.reason, '')
		FROM access_log l
		LEFT JOIN clients c ON c.id = l.client_id
		LEFT JOIN users u ON u.id = l.user_id
		%s AND l.status_code >= 400
		ORDER BY l.ts DESC, l.id DESC
		LIMIT $%d`, statsRouteExpr, statsAuthTypeExpr, statsKeyIDExpr, where, len(args))

	rows, err := s.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return APIStatsErrorsResponse{}, err
	}
	defer rows.Close()

	items := []APIStatsErrorItem{}
	for rows.Next() {
		var it APIStatsErrorItem
		if err := rows.Scan(&it.OccurredAt, &it.Method, &it.RouteTemplate, &it.StatusCode, &it.DurationMs,
			&it.AuthType, &it.APIKeyID, &it.APIKeyPrefix, &it.APIKeyName, &it.ClientIP, &it.ErrorCode); err != nil {
			return APIStatsErrorsResponse{}, err
		}
		items = append(items, it)
	}
	return APIStatsErrorsResponse{Items: items}, rows.Err()
}

func (s *pgStore) StatsAPIKeys(ctx context.Context, q APIStatsQuery) (APIStatsKeysResponse, error) {
	from, to, _ := q.ResolveWindow(time.Now().UTC())
	where, args := buildStatsWhere(q, from, to)
	out := APIStatsKeysResponse{Items: []APIStatsKeyItem{}}

	summarySQL := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE l.client_id IS NOT NULL OR l.user_id IS NOT NULL)::bigint,
			COUNT(DISTINCT %s) FILTER (WHERE l.client_id IS NOT NULL OR l.user_id IS NOT NULL)::bigint,
			COUNT(*) FILTER (WHERE l.status_code = 401 OR %s = 'invalid_key')::bigint,
			COUNT(*) FILTER (WHERE l.status_code = 403)::bigint
		FROM access_log l %s`, statsKeyIDExpr, statsAuthTypeExpr, where)
	if err := s.pool.QueryRow(ctx, summarySQL, args...).Scan(
		&out.Summary.TotalKeyRequests,
		&out.Summary.ActiveKeyCount,
		&out.Summary.Unauthorized401Count,
		&out.Summary.Forbidden403Count,
	); err != nil {
		return out, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	args = append(args, limit)
	itemsSQL := fmt.Sprintf(`
		SELECT
			%s AS key_id,
			CASE
				WHEN l.client_id IS NOT NULL THEN 'client#' || l.client_id::text
				ELSE 'user#' || l.user_id::text
			END AS key_prefix,
			CASE
				WHEN l.client_id IS NOT NULL THEN COALESCE(MAX(c.name), 'client#' || l.client_id::text)
				ELSE COALESCE(MAX(u.name), 'user#' || l.user_id::text) || ' (user-token)'
			END AS key_name,
			COALESCE(MAX(l.user_id)::text, '') AS uid,
			COALESCE(MAX(u.name), '') AS uname,
			CASE
				WHEN l.client_id IS NOT NULL AND MAX(c.status) = 'blocked' THEN 'revoked'
				ELSE 'active'
			END AS status,
			COUNT(*)::bigint AS cnt,
			COUNT(*) FILTER (WHERE l.status_code >= 400 AND l.status_code < 500)::bigint AS err4,
			COUNT(*) FILTER (WHERE l.status_code >= 500)::bigint AS err5,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8 AS p95_ms,
			MAX(l.ts) AS last_active
		FROM access_log l
		LEFT JOIN clients c ON c.id = l.client_id
		LEFT JOIN users u ON u.id = COALESCE(l.user_id, c.user_id)
		%s AND (l.client_id IS NOT NULL OR l.user_id IS NOT NULL)
		GROUP BY key_id, l.client_id, l.user_id
		ORDER BY cnt DESC, key_id ASC
		LIMIT $%d`, statsKeyIDExpr, where, len(args))

	rows, err := s.pool.Query(ctx, itemsSQL, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var it APIStatsKeyItem
		var last time.Time
		if err := rows.Scan(&it.APIKeyID, &it.APIKeyPrefix, &it.APIKeyName, &it.UserID, &it.UserName,
			&it.Status, &it.Count, &it.Error4xx, &it.Error5xx, &it.P95Ms, &last); err != nil {
			return out, err
		}
		if !last.IsZero() {
			t := last.UTC()
			it.LastActiveAt = &t
		}
		if out.Summary.TotalKeyRequests > 0 {
			it.ShareRate = float64(it.Count) / float64(out.Summary.TotalKeyRequests)
		}
		out.Items = append(out.Items, it)
	}
	return out, rows.Err()
}

func (s *pgStore) StatsAPIKeyDetail(ctx context.Context, keyID string, q APIStatsQuery) (APIStatsKeyDetailResponse, error) {
	q.ApiKeyID = keyID
	from, to, granularity := q.ResolveWindow(time.Now().UTC())
	where, args := buildStatsWhere(q, from, to)

	out := APIStatsKeyDetailResponse{
		KeyID:        keyID,
		KeyPrefix:    keyID,
		KeyName:      keyID,
		Status:       "active",
		Series:       []APIStatsSeriesPoint{},
		TopRoutes:    []APIStatsRouteItem{},
		ErrorRoutes:  []APIStatsRouteItem{},
		RecentErrors: []APIStatsErrorItem{},
	}
	if keyID == "__invalid__" {
		out.KeyPrefix = "invalid"
		out.KeyName = "无效 / 已吊销 Token 尝试"
		out.Status = "revoked"
	} else if raw, ok := strings.CutPrefix(keyID, "client:"); ok {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			out.KeyPrefix = fmt.Sprintf("client#%d", id)
			var name, status string
			err := s.pool.QueryRow(ctx, `SELECT name, status FROM clients WHERE id = $1`, id).Scan(&name, &status)
			if err == nil {
				out.KeyName = name
				if status == ClientStatusBlocked {
					out.Status = "revoked"
				}
			}
		}
	} else if raw, ok := strings.CutPrefix(keyID, "user:"); ok {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			out.KeyPrefix = fmt.Sprintf("user#%d", id)
			var name string
			if err := s.pool.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, id).Scan(&name); err == nil {
				out.KeyName = name + " (user-token)"
			}
		}
	}

	var total, err4, err5 int64
	var p95 float64
	var last *time.Time
	sumSQL := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 400 AND l.status_code < 500)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 500)::bigint,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8,
			MAX(l.ts)
		FROM access_log l %s`, where)
	if err := s.pool.QueryRow(ctx, sumSQL, args...).Scan(&total, &err4, &err5, &p95, &last); err != nil {
		return out, err
	}
	out.Summary = APIStatsKeyDetailSummary{
		TotalRequests: total,
		Error4xx:      err4,
		Error5xx:      err5,
		P95Ms:         p95,
		LastActiveAt:  last,
	}

	seriesSQL := fmt.Sprintf(`
		SELECT
			date_trunc('%s', l.ts AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket,
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 400 AND l.status_code < 500)::bigint,
			COUNT(*) FILTER (WHERE l.status_code >= 500)::bigint,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY l.duration_ms), 0)::float8
		FROM access_log l %s
		GROUP BY bucket
		ORDER BY bucket ASC`, granularity, where)
	sRows, err := s.pool.Query(ctx, seriesSQL, args...)
	if err != nil {
		return out, err
	}
	for sRows.Next() {
		var pt APIStatsSeriesPoint
		if err := sRows.Scan(&pt.Bucket, &pt.Requests, &pt.Error4xx, &pt.Error5xx, &pt.P95Ms); err != nil {
			sRows.Close()
			return out, err
		}
		out.Series = append(out.Series, pt)
	}
	if err := sRows.Err(); err != nil {
		sRows.Close()
		return out, err
	}
	sRows.Close()

	topResp, err := s.StatsRoutes(ctx, APIStatsQuery{
		Range: q.Range, From: from, To: to, Method: q.Method, Route: q.Route,
		StatusClass: q.StatusClass, AuthType: q.AuthType, ApiKeyID: keyID, Sort: "count", Limit: 20,
	})
	if err != nil {
		return out, err
	}
	out.TopRoutes = topResp.Items

	errResp, err := s.StatsRoutes(ctx, APIStatsQuery{
		Range: q.Range, From: from, To: to, Method: q.Method, Route: q.Route,
		StatusClass: q.StatusClass, AuthType: q.AuthType, ApiKeyID: keyID, Sort: "error_rate", Limit: 20,
	})
	if err != nil {
		return out, err
	}
	for _, r := range errResp.Items {
		if r.Error4xx > 0 || r.Error5xx > 0 {
			out.ErrorRoutes = append(out.ErrorRoutes, r)
		}
	}

	recentResp, err := s.StatsErrors(ctx, APIStatsQuery{
		Range: q.Range, From: from, To: to, Method: q.Method, Route: q.Route,
		StatusClass: q.StatusClass, AuthType: q.AuthType, ApiKeyID: keyID, Limit: 50,
	})
	if err != nil {
		return out, err
	}
	out.RecentErrors = recentResp.Items
	return out, nil
}
