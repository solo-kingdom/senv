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
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
  admin create-registration <user> [--expires 30m]
                             为用户签发一次性注册码（明文只展示一次）
  admin list-clients [--user <name>]
                             列出已注册 client
  admin block-client --client <name> [--user <user>]
                             屏蔽 client（其名下 token 立即失效，可解封）
  admin unblock-client --client <name> [--user <user>]
                             解封 client
  admin logs [--user u] [--client c] [--since d] [--until d] [--outcome o] [--limit n]
                             查询访问日志（时间/IP/身份/结果/原因，含日期；
                             outcome 含 OK/AUTH-FAILED/BLOCKED/RATE-LIMITED/ADMIN）
  admin logs-prune --before <YYYY-MM-DD>
                             清理该日期之前的访问日志

Flags（serve/migrate/admin 通用）:
  --dsn    Postgres 连接串（默认取环境变量 SENV_SERVER_DSN）
  --addr   监听地址（仅 serve，默认 ":8080"，环境变量 SENV_SERVER_ADDR 可覆盖）

serve 专属:
  --trust-proxy-headers  反代对端为 loopback/私网（同机或 docker 网桥）时采信
                         X-Real-IP/X-Forwarded-For（默认关闭）
  --logs-retain-days N   访问日志保留天数（默认 90，0 关闭自动清理）
  --alert-webhook URL    告警 webhook（默认取 SENV_SERVER_ALERT_WEBHOOK；
                         空则告警关闭）：连续爆破/屏蔽/新注册/换 IP 时 POST JSON
  --alert-auth-fail-threshold N
                         连续 AUTH-FAILED 告警阈值（默认 10）
  --alert-debounce D     同类型同对象最小告警间隔（默认 5m）

环境变量（serve）:
  SENV_SERVER_TOKEN_PEPPER  token 哈希 HMAC pepper（可选；空=旧 SHA-256 行为；
                             启用后存量 token 走回退比对，请按文档指引尽快轮换）
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
	historyRetain := fs.Int("history-retain", store.DefaultHistoryRetain,
		"history versions kept per entry (0 or negative disables entry history)")
	trustProxy := fs.Bool("trust-proxy-headers", false,
		"trust X-Real-IP/X-Forwarded-For only when the direct peer is loopback or a private address (same-host or private-network reverse proxy)")
	alertWebhook := fs.String("alert-webhook", os.Getenv("SENV_SERVER_ALERT_WEBHOOK"),
		"alert webhook URL (POST JSON on auth-failure storms, blocks, registrations, client IP changes); empty disables alerting")
	alertFailThreshold := fs.Int("alert-auth-fail-threshold", 10,
		"consecutive AUTH-FAILED per source IP before an alert fires")
	alertDebounce := fs.Duration("alert-debounce", 5*time.Minute,
		"minimum interval between repeated alerts of the same type and target")
	logsRetainDays := fs.Int("logs-retain-days", 90,
		"access log retention in days (0 disables automatic pruning)")
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

	// token 哈希 pepper（可选）：配置后 token 存 HMAC-SHA256(pepper, token)，
	// 空 = 原 SHA-256 行为。pepper 只进进程内存，绝不入库或入日志。
	pg := store.NewSQL(pool)
	if pep := os.Getenv("SENV_SERVER_TOKEN_PEPPER"); pep != "" {
		pg.SetTokenPepper([]byte(pep))
	}

	// 认证结果与 vault seq 走进程内缓存（decorator），对外仍是同一个
	// store.Store；失效广播监听在下方启动
	st := store.WithCache(pg)
	st.SetHistoryRetain(*historyRetain)

	srv := handler.New(st, handler.Options{
		MaxBodyBytes:           *maxBodyMB,
		AuthRateLimit:          *rateLimit,
		TrustProxyHeaders:      *trustProxy,
		AlertWebhook:           *alertWebhook,
		AlertAuthFailThreshold: *alertFailThreshold,
		AlertDebounce:          *alertDebounce,
	})

	// 访问日志自动清理：启动先跑一轮，之后每 24h 一轮；失败不致命，下轮重试。
	// 复用 handler 的同一 store 实例，不在进程内自建第二个。
	if *logsRetainDays > 0 {
		go pruneAccessLogsPeriodically(st, time.Duration(*logsRetainDays)*24*time.Hour)
	}

	// 认证缓存跨进程失效：admin 一次性进程 revoke/block/unblock 后经
	// pg_notify 广播，本进程监听收到即清空缓存；断线重连先全清，广播
	// 丢失由缓存 TTL 兜底（server-auth spec「认证结果缓存」）。
	listenerCtx, stopListener := context.WithCancel(context.Background())
	defer stopListener()
	go store.StartInvalidationListener(listenerCtx, *dsn, st.ClearAuth, time.Second)

	// last_seen 内存节流的周期落库：与既有 SQL 节流粒度（1 分钟）对齐；
	// 停机时在优雅停机后另有一次 best-effort flush
	touchCtx, stopTouchFlush := context.WithCancel(context.Background())
	defer stopTouchFlush()
	go st.StartTouchFlusher(touchCtx, time.Minute)

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

	// 优雅停机：SIGINT/SIGTERM 后排空在途请求（10s 上限），避免发版瞬断同步
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "优雅停机失败: %v\n", err)
		}
		// last_seen 内存缓冲停机前落库（best-effort，5s 上限；崩溃丢弃无实害）
		touchCtx, touchCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer touchCancel()
		st.FlushTouches(touchCtx)
	}()

	fmt.Printf("senv-server listening on %s\n", *addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "服务退出: %v\n", err)
		os.Exit(1)
	}
}

// pruneAccessLogsPeriodically 周期清理超过保留期的访问日志（复用分批删除）。
// best-effort：失败只记服务端日志，等下个周期重试。复用 serve 进程唯一的
// store 实例，不再自建。
func pruneAccessLogsPeriodically(st store.Store, retain time.Duration) {
	prune := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		n, err := st.PruneAccessLogs(ctx, time.Now().Add(-retain))
		if err != nil {
			slog.Error("access log prune failed", "err", err)
			return
		}
		if n > 0 {
			slog.Info("access log pruned", "rows", n)
		}
	}
	prune()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		prune()
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
	// 子命令私有 flag 必须在 Parse 前定义（Go flag 遇位置参数即停止），
	// 各子命令按需读取；未用到的定义无副作用
	expires := fs.String("expires", "30m", "注册码有效期（Go duration，如 30m、2h）")
	clientName := fs.String("client", "", "client 设备名")
	userFilter := fs.String("user", "", "限定用户名（缺省作用于全部用户）")
	outcome := fs.String("outcome", "", "访问日志结果过滤（OK/AUTH-FAILED/BLOCKED/RATE-LIMITED/ADMIN）")
	logsSince := fs.String("since", "", "起始日期（YYYY-MM-DD 或 RFC3339，含）")
	logsUntil := fs.String("until", "", "结束日期（YYYY-MM-DD 或 RFC3339，含当天）")
	logsBefore := fs.String("before", "", "清理该日期之前的日志（YYYY-MM-DD）")
	logsLimit := fs.Int("limit", 100, "最多显示条数")
	fs.Parse(args[1:])

	switch sub {
	case "create-registration":
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "用法: senv-server admin create-registration <username> [--expires 30m] [--dsn ...]")
			os.Exit(1)
		}
		requireDSN(*dsn)
		ttl, err := time.ParseDuration(*expires)
		if err != nil || ttl <= 0 {
			fmt.Fprintf(os.Stderr, "错误: --expires %q 不是正的有效时长\n", *expires)
			os.Exit(1)
		}
		withStore(*dsn, func(st store.Store) error {
			userID, err := st.UserIDByName(context.Background(), fs.Arg(0))
			if err != nil {
				return fmt.Errorf("用户 %q 不存在", fs.Arg(0))
			}
			code, err := st.CreateRegistrationCode(context.Background(), userID, ttl)
			if err != nil {
				return err
			}
			recordAdminAudit(st, "create-registration "+fs.Arg(0), userID, 0)
			// 明文注册码只在此展示一次，库中仅存 SHA-256 哈希
			fmt.Printf("✓ 已为用户 %q 签发一次性注册码（有效期 %s）:\n%s\n", fs.Arg(0), ttl, code)
			fmt.Println("在客户端执行: senv server register --address <server> --code <注册码> --name <设备名>")
			return nil
		})
	case "list-clients":
		withStore(*dsn, func(st store.Store) error {
			var userID int64 = -1
			if *userFilter != "" {
				id, err := st.UserIDByName(context.Background(), *userFilter)
				if err != nil {
					return fmt.Errorf("用户 %q 不存在", *userFilter)
				}
				userID = id
			}
			clients, err := st.ListClients(context.Background(), userID)
			if err != nil {
				return err
			}
			if len(clients) == 0 {
				fmt.Println("（无 client）")
				return nil
			}
			fmt.Printf("%-4s %-16s %-20s %-8s %-24s %-24s\n", "ID", "NAME", "USER", "STATUS", "CREATED", "LAST_SEEN")
			for _, c := range clients {
				lastSeen := "-"
				if c.LastSeenAt != nil {
					lastSeen = c.LastSeenAt.Format("2006-01-02 15:04:05")
				}
				fmt.Printf("%-4d %-16s %-20d %-8s %-24s %-24s\n",
					c.ID, c.Name, c.UserID, c.Status,
					c.CreatedAt.Format("2006-01-02 15:04:05"), lastSeen)
			}
			return nil
		})
	case "block-client", "unblock-client":
		status := store.ClientStatusBlocked
		hint := "已屏蔽"
		if sub == "unblock-client" {
			status = store.ClientStatusActive
			hint = "已解封"
		}
		if *clientName == "" {
			fmt.Fprintf(os.Stderr, "用法: senv-server admin %s --client <设备名> [--user <用户名>] [--dsn ...]\n", sub)
			os.Exit(1)
		}
		withStore(*dsn, func(st store.Store) error {
			var userID int64 = -1
			if *userFilter != "" {
				id, err := st.UserIDByName(context.Background(), *userFilter)
				if err != nil {
					return fmt.Errorf("用户 %q 不存在", *userFilter)
				}
				userID = id
			}
			if err := st.SetClientStatus(context.Background(), userID, *clientName, status); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("client %q 不存在（可用 admin list-clients 核对设备名）", *clientName)
				}
				return err
			}
			// 审计用 client id 尽力解析（失败不影响审计本身）
			if cid, err := resolveClientID(st, *clientName, *userFilter); err == nil {
				recordAdminAudit(st, fmt.Sprintf("%s user=%s client=%s", sub, *userFilter, *clientName), userIDOrZero(userID), cid)
			} else {
				recordAdminAudit(st, fmt.Sprintf("%s user=%s client=%s", sub, *userFilter, *clientName), userIDOrZero(userID), 0)
			}
			fmt.Printf("✓ client %q %s\n", *clientName, hint)
			return nil
		})
	case "logs":
		requireDSN(*dsn)
		withStore(*dsn, func(st store.Store) error {
			f := store.AccessLogFilter{Outcome: *outcome, Limit: *logsLimit}
			if *userFilter != "" {
				id, err := st.UserIDByName(context.Background(), *userFilter)
				if err != nil {
					return fmt.Errorf("用户 %q 不存在", *userFilter)
				}
				f.User = &id
			}
			if *clientName != "" {
				cid, err := resolveClientID(st, *clientName, *userFilter)
				if err != nil {
					return err
				}
				f.Client = &cid
			}
			if *logsSince != "" {
				t, err := parseAdminDate(*logsSince, false)
				if err != nil {
					return fmt.Errorf("--since %q: %w", *logsSince, err)
				}
				f.Since = &t
			}
			if *logsUntil != "" {
				t, err := parseAdminDate(*logsUntil, true)
				if err != nil {
					return fmt.Errorf("--until %q: %w", *logsUntil, err)
				}
				f.Until = &t
			}
			events, err := st.ListAccessLogs(context.Background(), f)
			if err != nil {
				return err
			}
			if len(events) == 0 {
				fmt.Println("（无匹配的访问日志）")
				return nil
			}
			fmt.Printf("%-20s %-15s %-28s %-14s %-12s %-13s %s\n",
				"时间", "IP", "METHOD PATH", "CLIENT", "USER", "结果", "原因")
			for _, e := range events {
				fmt.Printf("%-20s %-15s %-28s %-14s %-12s %-13s %s\n",
					e.Time.Local().Format("2006-01-02 15:04:05"),
					truncateCell(e.IP, 15),
					truncateCell(e.Method+" "+e.Path, 28),
					truncateCell(clientDisplay(e), 14),
					truncateCell(userDisplay(e), 12),
					e.Outcome, e.Reason)
			}
			return nil
		})
	case "logs-prune":
		requireDSN(*dsn)
		if *logsBefore == "" {
			fmt.Fprintln(os.Stderr, "用法: senv-server admin logs-prune --before <YYYY-MM-DD> [--dsn ...]")
			os.Exit(1)
		}
		before, err := parseAdminDate(*logsBefore, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: --before %q: %v\n", *logsBefore, err)
			os.Exit(1)
		}
		withStore(*dsn, func(st store.Store) error {
			n, err := st.PruneAccessLogs(context.Background(), before)
			if err != nil {
				return err
			}
			fmt.Printf("✓ 已删除 %d 条 %s 之前的访问日志\n", n, before.Format("2006-01-02 15:04:05"))
			return nil
		})
	case "create-user":
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "用法: senv-server admin create-user <name> [--dsn ...]")
			os.Exit(1)
		}
		requireDSN(*dsn)
		withStore(*dsn, func(st store.Store) error {
			token, err := st.CreateUser(context.Background(), fs.Arg(0))
			if err != nil {
				return err
			}
			uid, err := st.UserIDByName(context.Background(), fs.Arg(0))
			if err != nil {
				uid = 0
			}
			recordAdminAudit(st, "create-user "+fs.Arg(0), uid, 0)
			// 明文 token 只在此展示一次，库中仅存 SHA-256 哈希
			fmt.Printf("✓ 用户 %q 已创建\nToken（仅展示一次，请妥善保存）:\n%s\n", fs.Arg(0), token)
			return nil
		})
	case "revoke-token":
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "用法: senv-server admin revoke-token <token|-> [--dsn ...]")
			os.Exit(1)
		}
		requireDSN(*dsn)
		tokenArg := fs.Arg(0)
		// "-" 从 stdin 读 token：多用户主机上避免明文出现在进程列表与
		// Shell 历史（echo <token> | senv-server admin revoke-token -）
		if tokenArg == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "读取 stdin 失败: %v\n", err)
				os.Exit(1)
			}
			tokenArg = strings.TrimSpace(string(data))
			if tokenArg == "" {
				fmt.Fprintln(os.Stderr, "错误: stdin 未提供 token")
				os.Exit(1)
			}
		}
		withStore(*dsn, func(st store.Store) error {
			if err := st.RevokeToken(context.Background(), tokenArg); err != nil {
				return err
			}
			uid, err := st.UserIDByToken(context.Background(), tokenArg)
			if err != nil {
				uid = 0
			}
			recordAdminAudit(st, "revoke-token", uid, 0)
			fmt.Println("✓ token 已吊销")
			return nil
		})
	default:
		usage()
		os.Exit(1)
	}
}

// recordAdminAudit best-effort 写一条 ADMIN 审计事件：记录操作类型与目标对象。
// 失败仅记服务端日志，不影响命令结果与退出码（access-log spec「管理员操作审计」）。
// userID/clientID 填被操作对象（解析不到时传 0）。
func recordAdminAudit(st store.Store, reason string, userID, clientID int64) {
	err := st.RecordAccess(context.Background(), store.AccessEvent{
		IP: "-", Method: "ADMIN", Path: "admin",
		Outcome: store.AccessOutcomeAdmin, Reason: reason,
		UserID: userID, ClientID: clientID,
	})
	if err != nil {
		slog.Error("admin audit write failed", "reason", reason, "err", err)
	}
}

func withStore(dsn string, fn func(store.Store) error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := fn(store.NewSQL(pool)); err != nil {
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

// userIDOrZero 把 admin 过滤用的 -1 哨兵（全部用户）规范为 0（未知）
func userIDOrZero(id int64) int64 {
	if id < 0 {
		return 0
	}
	return id
}

// resolveClientID 按设备名解析 client id；同名跨用户时要求显式 --user
func resolveClientID(st store.Store, name, userName string) (int64, error) {
	var userID int64 = -1
	if userName != "" {
		id, err := st.UserIDByName(context.Background(), userName)
		if err != nil {
			return 0, fmt.Errorf("用户 %q 不存在", userName)
		}
		userID = id
	}
	clients, err := st.ListClients(context.Background(), userID)
	if err != nil {
		return 0, err
	}
	var matches []store.Client
	for _, c := range clients {
		if c.Name == name {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0].ID, nil
	case 0:
		return 0, fmt.Errorf("client %q 不存在（可用 admin list-clients 核对）", name)
	default:
		return 0, fmt.Errorf("client %q 归属多个用户，请加 --user 限定", name)
	}
}

// parseAdminDate 解析 YYYY-MM-DD（本地时区）或 RFC3339；endOfDay 用于 until 含当天
func parseAdminDate(value string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		if endOfDay {
			return t.Add(24*time.Hour - time.Second), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("无法解析日期，支持 YYYY-MM-DD 或 RFC3339")
}

func truncateCell(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func clientDisplay(e store.AccessEventRow) string {
	if e.ClientName != "" {
		return e.ClientName
	}
	if e.ClientID > 0 {
		return fmt.Sprintf("#%d", e.ClientID)
	}
	return "-"
}

func userDisplay(e store.AccessEventRow) string {
	if e.UserName != "" {
		return e.UserName
	}
	if e.UserID > 0 {
		return fmt.Sprintf("#%d", e.UserID)
	}
	return "-"
}
