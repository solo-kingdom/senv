// 系统日志（Access Log）存储：每请求安全事件落库、过滤查询与按日期清理。
// 事件只含连接与身份元数据（时间、IP、方法路径、client/user、结果、原因），
// 绝不记录 token、口令或密文内容。
package store

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"
)

// 访问结果取值
const (
	AccessOutcomeOK          = "OK"
	AccessOutcomeAuthFailed  = "AUTH-FAILED"
	AccessOutcomeBlocked     = "BLOCKED"
	AccessOutcomeRateLimited = "RATE-LIMITED"
	// AccessOutcomeAdmin 标记 admin CLI 操作审计事件（create-user / revoke /
	// create-registration / block / unblock），与请求事件同表存储
	AccessOutcomeAdmin = "ADMIN"
)

// 访问日志字段长度上限。path 来自请求 URL，认证前即可被攻击者注入最长
// MaxHeaderBytes 量级的超长值，不截断则未认证流量可灌爆日志表；ip/reason
// 为服务端可控，防御性同限。
const (
	MaxAccessLogIPBytes     = 64
	MaxAccessLogPathBytes   = 512
	MaxAccessLogReasonBytes = 128
)

// AccessEvent 是一条安全事件
type AccessEvent struct {
	Time     time.Time `json:"time"`
	IP       string    `json:"ip"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	ClientID int64     `json:"client_id"` // 0 = 未知（无 client 归属或认证失败）
	UserID   int64     `json:"user_id"`   // 0 = 未知
	Outcome  string    `json:"outcome"`
	Reason   string    `json:"reason,omitempty"`
}

// AccessEventRow 是带名称的查询结果行（client/user 以名称展示便于管理员阅读）
type AccessEventRow struct {
	AccessEvent
	ClientName string `json:"client_name,omitempty"`
	UserName   string `json:"user_name,omitempty"`
}

// truncateUTF8 把 s 截断到最多 max 字节，不切断尾部的多字节字符。
// UTF-8 最长 4 字节，循环至多回退 3 次。
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// RecordAccess 落一条安全事件（调用方 best-effort：失败仅记服务端日志）。
// e.Time 为零值时取当前时刻；变长字段入库前截断（见 MaxAccessLog* 常量）。
func (s *pgStore) RecordAccess(ctx context.Context, e AccessEvent) error {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.IP = truncateUTF8(e.IP, MaxAccessLogIPBytes)
	e.Path = truncateUTF8(e.Path, MaxAccessLogPathBytes)
	e.Reason = truncateUTF8(e.Reason, MaxAccessLogReasonBytes)
	if e.Outcome != AccessOutcomeOK && e.Outcome != AccessOutcomeAuthFailed &&
		e.Outcome != AccessOutcomeBlocked && e.Outcome != AccessOutcomeRateLimited &&
		e.Outcome != AccessOutcomeAdmin {
		return validationErrorf("未知的访问结果 %q", e.Outcome)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO access_log (ts, ip, method, path, client_id, user_id, outcome, reason)
		 VALUES ($1, $2, $3, $4, NULLIF($5, 0), NULLIF($6, 0), $7, NULLIF($8, ''))`,
		e.Time, e.IP, e.Method, e.Path, e.ClientID, e.UserID, e.Outcome, e.Reason)
	return err
}

// AccessLogFilter 过滤查询；指针为 nil 表示不过滤（0 值 id 是合法的未知身份）
type AccessLogFilter struct {
	User    *int64
	Client  *int64
	Since   *time.Time
	Until   *time.Time
	Outcome string
	Limit   int
}

// ListAccessLogs 按过滤条件查询安全事件，时间新到旧，带 client/user 名称
func (s *pgStore) ListAccessLogs(ctx context.Context, f AccessLogFilter) ([]AccessEventRow, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}

	where := `WHERE true`
	args := []any{}
	addCond := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND %s = $%d", cond, len(args))
	}
	if f.User != nil {
		addCond("l.user_id", *f.User)
	}
	if f.Client != nil {
		addCond("l.client_id", *f.Client)
	}
	if f.Outcome != "" {
		addCond("l.outcome", f.Outcome)
	}
	if f.Since != nil {
		args = append(args, *f.Since)
		where += fmt.Sprintf(" AND l.ts >= $%d", len(args))
	}
	if f.Until != nil {
		args = append(args, *f.Until)
		where += fmt.Sprintf(" AND l.ts <= $%d", len(args))
	}
	args = append(args, f.Limit)

	rows, err := s.pool.Query(ctx,
		`SELECT l.ts, l.ip, l.method, l.path, COALESCE(l.client_id, 0), COALESCE(l.user_id, 0),
		        l.outcome, COALESCE(l.reason, ''), COALESCE(c.name, ''), COALESCE(u.name, '')
		 FROM access_log l
		 LEFT JOIN clients c ON c.id = l.client_id
		 LEFT JOIN users u ON u.id = l.user_id
		 `+where+fmt.Sprintf(" ORDER BY l.ts DESC, l.id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []AccessEventRow{}
	for rows.Next() {
		var e AccessEventRow
		if err := rows.Scan(&e.Time, &e.IP, &e.Method, &e.Path, &e.ClientID, &e.UserID,
			&e.Outcome, &e.Reason, &e.ClientName, &e.UserName); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// PruneAccessLogs 分批删除 before 之前的所有事件，返回删除总数。
// 分批避免长事务与大范围锁表。
func (s *pgStore) PruneAccessLogs(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	for {
		tag, err := s.pool.Exec(ctx,
			`DELETE FROM access_log WHERE id IN (
			   SELECT id FROM access_log WHERE ts < $1 LIMIT 1000
			 )`, before)
		if err != nil {
			return total, err
		}
		n := tag.RowsAffected()
		total += n
		if n < 1000 {
			return total, nil
		}
	}
}
