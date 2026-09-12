// 认证缓存的跨进程失效广播：admin 一次性进程（revoke/block/unblock）经
// pg_notify 通知 serve 进程清空进程内认证缓存。admin 与 serve 是不同进程
// （无 HTTP admin 端点，已验证路由表），同进程失效不可达——这是单实例
// 部署下唯一的跨进程失效通道（driver server-storage-cache grill D8）。
// 广播丢失（监听连接瞬断）由认证缓存 TTL 兜底，语义见 server-auth spec
// 「认证结果缓存」与 ADR server-cache-out-of-band-invalidation。
package store

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// invalidationChannel 是认证缓存失效广播的 LISTEN/NOTIFY 通道名
const invalidationChannel = "senv_cache_invalidate"

// notifyCacheInvalidation 广播认证缓存失效（admin 写路径用，best-effort）。
// 失败仅记日志不回滚业务写入：广播丢失由消费端 TTL 兜底，吊销/屏蔽的
// 库内事实不受影响。
func (s *pgStore) notifyCacheInvalidation(ctx context.Context) {
	if _, err := s.pool.Exec(ctx, `SELECT pg_notify($1, '')`, invalidationChannel); err != nil {
		slog.Error("broadcast cache invalidation failed", "err", err)
	}
}

// StartInvalidationListener 监听失效广播，收到即调用 onInvalidate（serve
// 进程传 cachedStore.ClearAuth）。持有专用连接——pgx 的 LISTEN 与
// WaitForNotification 必须在同一连接上。断线后按 reconnectAfter 间隔重连，
// 重连成功先触发一次 onInvalidate，压缩断连期间丢失广播的陈旧窗口。
// 阻塞至 ctx 取消；调用方负责放入独立 goroutine。
func StartInvalidationListener(ctx context.Context, dsn string, onInvalidate func(), reconnectAfter time.Duration) {
	for ctx.Err() == nil {
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			slog.Warn("invalidation listener connect failed", "err", err)
			if !sleepBeforeReconnect(ctx, reconnectAfter) {
				return
			}
			continue
		}
		if _, err := conn.Exec(ctx, `LISTEN `+invalidationChannel); err != nil {
			slog.Warn("invalidation listener subscribe failed", "err", err)
			conn.Close(context.Background())
			if !sleepBeforeReconnect(ctx, reconnectAfter) {
				return
			}
			continue
		}
		onInvalidate() // 重连成功即全清：断连期间丢失的广播由本次全清兜住
		for {
			if _, err := conn.WaitForNotification(ctx); err != nil {
				break
			}
			onInvalidate()
		}
		if ctx.Err() != nil {
			conn.Close(context.Background())
			return
		}
		slog.Warn("invalidation listener connection lost; reconnecting")
		conn.Close(context.Background())
		if !sleepBeforeReconnect(ctx, reconnectAfter) {
			return
		}
	}
}

// sleepBeforeReconnect 等待重连间隔；ctx 取消返回 false（进程退出语义）
func sleepBeforeReconnect(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
