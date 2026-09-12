// Package testdb 提供测试用临时 Postgres（testcontainers），docker 不可用时跳过。
package testdb

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/wii/senv/internal/server/migrate"
)

// New 启动一个临时 Postgres 容器，应用全部 schema 迁移后返回连接池。
// docker 不可用或容器启动失败时跳过测试。接受 testing.TB 以便 benchmark 复用。
func New(t testing.TB) *pgxpool.Pool {
	pool, _ := NewWithDSN(t)
	return pool
}

// NewWithDSN 同 New，但额外返回连接串（失效广播 LISTEN 需要独立于连接池的
// 专用连接）。
func NewWithDSN(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("senv_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
	if err != nil {
		t.Skipf("无法启动临时 Postgres（需要 docker）: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := migrate.Apply(ctx, conn); err != nil {
		conn.Close(ctx)
		t.Fatalf("migrate: %v", err)
	}
	conn.Close(ctx)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, dsn
}
