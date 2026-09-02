// senv-server 是 senv 的零知识密文托管服务端（独立二进制）。
//
// 子命令：
//
//	serve                     启动 HTTP 服务（启动前校验 schema 版本）
//	migrate                   应用数据库 schema 迁移
//	admin create-user <name>  创建用户并签发一次性明文 token（库中只存哈希）
//	admin revoke-token <tok>  吊销指定 token
//
// 数据库连接串通过 --dsn 或环境变量 SENV_SERVER_DSN 提供。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wii/senv/internal/server/handler"
	"github.com/wii/senv/internal/server/migrate"
	"github.com/wii/senv/internal/server/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "migrate":
		runMigrate(os.Args[2:])
	case "admin":
		runAdmin(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: senv-server <command> [flags]

Commands:
  serve                      启动 HTTP 服务
  migrate                    应用数据库 schema 迁移
  admin create-user <name>   创建用户并签发 token（明文只展示一次）
  admin revoke-token <token> 吊销 token

Flags（serve/migrate/admin 通用）:
  --dsn    Postgres 连接串（默认取环境变量 SENV_SERVER_DSN）
  --addr   监听地址（仅 serve，默认 ":8080"，环境变量 SENV_SERVER_ADDR 可覆盖）
`)
}

// dsnFrom 解析 --dsn 标志，缺省回落到环境变量
func dsnFrom(args []string, fs *flag.FlagSet) *string {
	return fs.String("dsn", os.Getenv("SENV_SERVER_DSN"), "Postgres DSN")
}

func requireDSN(dsn string) {
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "错误: 缺少数据库连接串，请提供 --dsn 或设置 SENV_SERVER_DSN")
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dsn := dsnFrom(args, fs)
	addr := fs.String("addr", envOr("SENV_SERVER_ADDR", ":8080"), "listen address")
	maxBodyMB := fs.Int64("max-body-bytes", 64<<20,
		"max request body size in bytes (must cover batch pushes; 64MB covers the 1000x512KB maximum)")
	rateLimit := fs.Int("auth-rate-limit", 30,
		"allowed auth failures per minute per source IP (negative disables the limiter)")
	fs.Parse(args)
	requireDSN(*dsn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 启动前校验 schema 版本，不匹配则拒绝启动并提示迁移方式
	conn, err := pgx.Connect(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	if err := migrate.CheckCurrent(ctx, conn); err != nil {
		conn.Close(ctx)
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	conn.Close(ctx)

	pool, err := pgxpool.New(context.Background(), *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建连接池失败: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	srv := handler.New(store.New(pool), handler.Options{
		MaxBodyBytes:  *maxBodyMB,
		AuthRateLimit: *rateLimit,
	})

	// 显式超时：慢连接（不完整的请求头/请求体）在超时后被回收，
	// 而不是无限占用连接与内存。64MB 批量推送在慢链路上可能耗时较长，
	// 读超时因此放宽到 2 分钟。
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	fmt.Printf("senv-server listening on %s\n", *addr)
	if err := httpServer.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "服务退出: %v\n", err)
		os.Exit(1)
	}
}

func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dsn := dsnFrom(args, fs)
	fs.Parse(args)
	requireDSN(*dsn)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	if err := migrate.Apply(ctx, conn); err != nil {
		fmt.Fprintf(os.Stderr, "迁移失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ schema 迁移完成")
}

func runAdmin(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(1)
	}
	sub := args[0]
	fs := flag.NewFlagSet("admin", flag.ExitOnError)
	dsn := dsnFrom(args[1:], fs)
	fs.Parse(args[1:])

	switch sub {
	case "create-user":
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "用法: senv-server admin create-user <name> [--dsn ...]")
			os.Exit(1)
		}
		requireDSN(*dsn)
		withStore(*dsn, func(st *store.Store) error {
			token, err := st.CreateUser(context.Background(), fs.Arg(0))
			if err != nil {
				return err
			}
			// 明文 token 只在此展示一次，库中仅存 SHA-256 哈希
			fmt.Printf("✓ 用户 %q 已创建\nToken（仅展示一次，请妥善保存）:\n%s\n", fs.Arg(0), token)
			return nil
		})
	case "revoke-token":
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "用法: senv-server admin revoke-token <token> [--dsn ...]")
			os.Exit(1)
		}
		requireDSN(*dsn)
		withStore(*dsn, func(st *store.Store) error {
			if err := st.RevokeToken(context.Background(), fs.Arg(0)); err != nil {
				return err
			}
			fmt.Println("✓ token 已吊销")
			return nil
		})
	default:
		usage()
		os.Exit(1)
	}
}

func withStore(dsn string, fn func(*store.Store) error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := fn(store.New(pool)); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
