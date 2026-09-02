// Package migrate 内置 senv-server 的 schema 顺序迁移与版本校验。
package migrate

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// LatestVersion 返回内置迁移的最新版本号
func LatestVersion() (int, error) {
	versions, err := migrationVersions()
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("no embedded migrations found")
	}
	return versions[len(versions)-1], nil
}

// migrationVersions 按文件名前缀解析并排序所有迁移版本
func migrationVersions() ([]int, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var versions []int
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "%04d_", &v); err != nil {
			return nil, fmt.Errorf("invalid migration filename %q: %w", e.Name(), err)
		}
		versions = append(versions, v)
	}
	sort.Ints(versions)
	return versions, nil
}

// ensureMigrationsTable 创建迁移记录表（幂等）
func ensureMigrationsTable(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

// CurrentVersion 返回数据库已应用的最新版本；未初始化时返回 0
func CurrentVersion(ctx context.Context, db *pgx.Conn) (int, error) {
	var version int
	err := db.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		// 表不存在（42P01）视为未初始化
		if strings.Contains(err.Error(), "schema_migrations") {
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}

// Apply 依次应用所有未执行的迁移
func Apply(ctx context.Context, db *pgx.Conn) error {
	if _, err := LatestVersion(); err != nil {
		return err
	}
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	versions, err := migrationVersions()
	if err != nil {
		return err
	}
	for _, v := range versions {
		if v <= current {
			continue
		}
		filename, err := migrationFilename(v)
		if err != nil {
			return err
		}
		sql, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return err
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		if err := ensureMigrationsTable(ctx, tx); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %d failed: %w", v, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, v); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CheckCurrent 校验数据库版本是否与 server 要求一致，不一致时给出迁移指引
func CheckCurrent(ctx context.Context, db *pgx.Conn) error {
	latest, err := LatestVersion()
	if err != nil {
		return err
	}
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	if current != latest {
		return fmt.Errorf("数据库 schema 版本不匹配: 当前 %d，要求 %d。请先运行: senv-server migrate", current, latest)
	}
	return nil
}

func migrationFilename(version int) (string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("%04d_", version)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("migration %d not found", version)
}
