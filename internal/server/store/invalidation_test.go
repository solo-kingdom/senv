// 失效广播的集成测试（testcontainers，任务 2.3）：复刻真实拓扑——
// cached decorator 扮演「serve 进程」，独立 pgStore 实例扮演「admin 进程」，
// 两者只共享 Postgres。吊销/屏蔽/解封在 admin 侧执行后，serve 侧缓存
// 只能靠 pg_notify → LISTEN 广播失效；spec 断言为「吊销命令完成后 ≤1 个
// 请求内生效」，测试以轮询到首个新鲜读为判据。
package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/testdb"
)

func waitForBroadcast(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestInvalidationBroadcast(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	admin := NewSQL(pool)
	cached := WithCache(NewSQL(pool))

	var invalidations atomic.Int64
	listenerCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go StartInvalidationListener(listenerCtx, dsn, func() {
		invalidations.Add(1)
		cached.ClearAuth()
	}, 100*time.Millisecond)

	// 准备：bob 注册 client（全部走 admin 实例，serve 侧仅被动缓存）
	if _, err := admin.CreateUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	bobID, err := admin.UserIDByName(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	code, err := admin.CreateRegistrationCode(ctx, bobID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	clientToken, _, err := admin.RegisterClient(ctx, code, "laptop")
	if err != nil {
		t.Fatal(err)
	}

	// 监听就绪探测：block→unblock 两次广播，计数上涨证明 LISTEN 已建立，
	// 后续场景不再受监听建连时序影响
	if err := admin.SetClientStatus(ctx, -1, "laptop", ClientStatusBlocked); err != nil {
		t.Fatal(err)
	}
	if err := admin.SetClientStatus(ctx, -1, "laptop", ClientStatusActive); err != nil {
		t.Fatal(err)
	}
	waitForBroadcast(t, func() bool { return invalidations.Load() >= 2 }, "listener 未就绪：广播未到达")

	// 场景一：吊销即时生效（spec：吊销命令完成后 ≤1 个请求内 401）
	aliceToken, err := admin.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := cached.AuthenticateWithClient(ctx, aliceToken); err != nil {
			t.Fatalf("warm auth %d: %v", i, err)
		}
	}
	if err := admin.RevokeToken(ctx, aliceToken); err != nil {
		t.Fatal(err)
	}
	waitForBroadcast(t, func() bool {
		_, err := cached.AuthenticateWithClient(ctx, aliceToken)
		return errors.Is(err, ErrNotFound)
	}, "吊销后 token 仍能通过缓存认证：广播失效")

	// 场景二：屏蔽穿透缓存（下一新鲜读即 ClientBlocked，不依赖 TTL 过期）
	for i := 0; i < 2; i++ {
		res, err := cached.AuthenticateWithClient(ctx, clientToken)
		if err != nil {
			t.Fatal(err)
		}
		if res.ClientBlocked {
			t.Fatalf("屏蔽前 auth = %+v, ClientBlocked 应为 false", res)
		}
	}
	if err := admin.SetClientStatus(ctx, -1, "laptop", ClientStatusBlocked); err != nil {
		t.Fatal(err)
	}
	waitForBroadcast(t, func() bool {
		res, err := cached.AuthenticateWithClient(ctx, clientToken)
		return err == nil && res.ClientBlocked
	}, "屏蔽后缓存认证结果未翻转为 blocked：广播失效")

	// 场景三：解封恢复（解封同样必须广播，否则已缓存的 403 滞留到 TTL）
	if err := admin.SetClientStatus(ctx, -1, "laptop", ClientStatusActive); err != nil {
		t.Fatal(err)
	}
	waitForBroadcast(t, func() bool {
		res, err := cached.AuthenticateWithClient(ctx, clientToken)
		return err == nil && !res.ClientBlocked
	}, "解封后缓存认证结果未恢复：广播失效")
}

// TestListenerReconnectClearsCache 验证断线自愈：从 PG 侧强制终止监听连接
// （复刻网络瞬断），listener 应自动重连，且重连成功即触发一次全清——
// 断连期间丢失的广播由本次全清兜住（driver 验收：重连时全清缓存）。
func TestListenerReconnectClearsCache(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	admin := NewSQL(pool)
	cached := WithCache(NewSQL(pool))

	var invalidations atomic.Int64
	listenerCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go StartInvalidationListener(listenerCtx, dsn, func() {
		invalidations.Add(1)
		cached.ClearAuth()
	}, 100*time.Millisecond)

	// 首条广播使 LISTEN 就绪
	if err := admin.RevokeToken(ctx, mustFreshToken(t, admin, "warmup")); err != nil {
		t.Fatal(err)
	}
	waitForBroadcast(t, func() bool { return invalidations.Load() >= 1 }, "listener 未就绪：广播未到达")

	// 从 PG 侧终止监听连接（其最后语句是 LISTEN，借此识别 pid）
	if _, err := pool.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query = 'LISTEN senv_cache_invalidate'`); err != nil {
		t.Fatal(err)
	}
	// 重连成功应触发一次全清（计数继续上涨，无需新广播）
	before := invalidations.Load()
	waitForBroadcast(t, func() bool { return invalidations.Load() >= before+1 }, "监听连接被终止后未重连或未全清")

	// 重连后的新连接仍能收到广播
	if err := admin.RevokeToken(ctx, mustFreshToken(t, admin, "alice")); err != nil {
		t.Fatal(err)
	}
	after := invalidations.Load()
	waitForBroadcast(t, func() bool { return invalidations.Load() >= after+1 }, "重连后广播未到达")
}

// mustFreshToken 签发全新用户并返回 token（吊销是一次性动作，轮询与
// 多次触发需要各自独立的 token）
func mustFreshToken(t *testing.T, st *pgStore, name string) string {
	t.Helper()
	// 用户名唯一约束：带时间戳避免重名
	token, err := st.CreateUser(context.Background(), fmt.Sprintf("%s-%d", name, time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
