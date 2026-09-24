// 受限角色必须读得到 schema_migrations：roles.sql 漏授权时，serve 启动的
// migrate.CheckCurrent 会以「schema 版本不匹配」退出（2026-09-24 部署前发现）。
package store

import (
	"context"
	"testing"

	"github.com/wii/senv/internal/server/migrate"
	"github.com/wii/senv/internal/server/testdb"
)

func TestRestrictedRolePassesSchemaVersionCheck(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	role := "senv_server_version_test"
	// 幂等清理：容器可能被会话复用（角色已存在且带授权依赖）
	mustExec(t, ctx, pool, `DO $$
BEGIN
	IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+role+`') THEN
		REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM `+role+`;
		REVOKE ALL PRIVILEGES ON SCHEMA public FROM `+role+`;
		DROP ROLE `+role+`;
	END IF;
END $$;`)
	mustExec(t, ctx, pool, "CREATE ROLE "+role+" LOGIN PASSWORD 'test'")
	t.Cleanup(func() {
		c := context.Background()
		mustExec(t, c, pool, "REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM "+role)
		mustExec(t, c, pool, "REVOKE ALL PRIVILEGES ON SCHEMA public FROM "+role)
		mustExec(t, c, pool, "DROP ROLE IF EXISTS "+role)
	})
	mustExec(t, ctx, pool, "GRANT USAGE ON SCHEMA public TO "+role)
	mustExec(t, ctx, pool, "GRANT SELECT ON schema_migrations TO "+role)

	rp, err := poolAsRole(ctx, pool, role, "test")
	if err != nil {
		t.Fatalf("以受限角色连接: %v", err)
	}
	defer rp.Close()

	acq, err := rp.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer acq.Release()

	latest, err := migrate.LatestVersion()
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	cur, err := migrate.CurrentVersion(ctx, acq.Conn())
	if err != nil {
		t.Fatalf("受限角色下 CurrentVersion 应成功: %v", err)
	}
	if cur != latest {
		t.Errorf("版本校验: got %d, want %d", cur, latest)
	}

	// 撤权必须以权限错误失败——曾被按错误信息子串误判成「未初始化」返回 0，
	// 让 serve 报出误导性的「schema 版本不匹配，请先运行 migrate」后退出
	mustExec(t, ctx, pool, "REVOKE SELECT ON schema_migrations FROM "+role)
	if _, err := migrate.CurrentVersion(ctx, acq.Conn()); err == nil {
		t.Fatal("撤权后 CurrentVersion 应报权限错误，不得静默返回 0")
	} else if !isPermissionDenied(err) {
		t.Errorf("应报 42501 权限不足: %v", err)
	}
}
