// 受限角色集成测试：serve 运行时角色对 access_log 仅 INSERT+SELECT，
// UPDATE/DELETE 必须被数据库拒绝（roles.sql 模板的可执行验证）。
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wii/senv/internal/server/testdb"
)

func TestRestrictedRoleCannotTamperAccessLog(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	// 超户建受限角色并按 senv-server/sql/roles.sql 的 access_log 部分授权
	role := "senv_server_restricted_test"
	// 幂等清理：容器可能被会话复用（角色已存在且带授权依赖）
	mustExec(t, ctx, pool, `DO $$
BEGIN
	IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+role+`') THEN
		REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM `+role+`;
		REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM `+role+`;
		REVOKE ALL PRIVILEGES ON SCHEMA public FROM `+role+`;
		DROP ROLE `+role+`;
	END IF;
END $$;`)
	mustExec(t, ctx, pool, "CREATE ROLE "+role+" LOGIN PASSWORD 'test'")
	t.Cleanup(func() {
		ctx := context.Background()
		mustExec(t, ctx, pool, "REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM "+role)
		mustExec(t, ctx, pool, "REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM "+role)
		mustExec(t, ctx, pool, "REVOKE ALL PRIVILEGES ON SCHEMA public FROM "+role)
		mustExec(t, ctx, pool, "DROP ROLE IF EXISTS "+role)
	})
	mustExec(t, ctx, pool, "GRANT USAGE ON SCHEMA public TO "+role)
	mustExec(t, ctx, pool, "GRANT SELECT, INSERT ON access_log TO "+role)
	// id 自增序列：与 roles.sql 的 ALL SEQUENCES 授权对齐
	mustExec(t, ctx, pool, "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO "+role)

	// 以受限角色连接（dsn 换用户）
	rp, err := poolAsRole(ctx, pool, role, "test")
	if err != nil {
		t.Fatalf("以受限角色连接: %v", err)
	}
	defer rp.Close()

	// INSERT 成功（serve 正常记录）
	if _, err := rp.Exec(ctx,
		`INSERT INTO access_log (ts, ip, method, path, outcome) VALUES ($1, $2, $3, $4, $5)`,
		time.Now(), "10.0.0.1", "GET", "/v1/healthz", AccessOutcomeOK); err != nil {
		t.Fatalf("受限角色 INSERT 应成功: %v", err)
	}
	var cnt int
	if err := rp.QueryRow(ctx, `SELECT count(*) FROM access_log`).Scan(&cnt); err != nil {
		t.Fatalf("受限角色 SELECT 应成功: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("count = %d, want 1", cnt)
	}

	// UPDATE/DELETE 被拒绝（抹除痕迹的最小攻击面）
	if _, err := rp.Exec(ctx, `UPDATE access_log SET outcome = $1`, AccessOutcomeOK); err == nil {
		t.Fatal("受限角色 UPDATE access_log 应被拒绝")
	} else if !isPermissionDenied(err) {
		t.Fatalf("UPDATE 错误应为权限拒绝, got: %v", err)
	}
	if _, err := rp.Exec(ctx, `DELETE FROM access_log`); err == nil {
		t.Fatal("受限角色 DELETE access_log 应被拒绝")
	} else if !isPermissionDenied(err) {
		t.Fatalf("DELETE 错误应为权限拒绝, got: %v", err)
	}
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// poolAsRole 以指定角色新开连接池：dsn 复用超户连接串仅替换用户名/密码
func poolAsRole(ctx context.Context, super *pgxpool.Pool, role, password string) (*pgxpool.Pool, error) {
	cfg := super.Config()
	connString := cfg.ConnString()
	// 简化：testdb 的 dsn 形如 postgres://test:test@host:port/senv_test?...
	parts := strings.SplitN(connString, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("无法从连接串解析主机段: %q", connString)
	}
	dsn := "postgres://" + role + ":" + password + "@" + parts[1]
	return pgxpool.New(ctx, dsn)
}

// isPermissionDenied 判断 pg 权限错误（42501 insufficient_privilege）
func isPermissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42501"
	}
	return strings.Contains(err.Error(), "42501") || strings.Contains(err.Error(), "permission denied")
}
