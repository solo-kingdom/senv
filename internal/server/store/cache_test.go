// 缓存 decorator 的单测（任务 1.2，假内层、注入时钟）与跨进程失效广播的
// 集成测试（任务 2.3，testcontainers 真 Postgres）。spec 场景对应关系：
//   - 缓存命中不改变认证语义 / 不产生认证 SQL 查询 → TestCacheAuthHit...
//   - 兜底窗口有界（TTL 过期回库）→ TestCacheAuthExpiry
//   - 吊销即时生效 / 屏蔽穿透缓存 / 解封恢复 → TestInvalidationBroadcast
package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeInner 是缓存单测的内层假实现：只重写被缓存路径触达的方法，其余
// 经由 nil 接口在误用时 panic 暴露问题。
type fakeInner struct {
	Store

	authCalls  atomic.Int32
	pullCalls  atomic.Int32
	authResult AuthResult
	authErr    error
	entries    []Entry
	pullLatest int64
	pushLatest int64

	retainN int

	touchMu    sync.Mutex
	touchCalls int
	touchedIDs []int64
}

func (f *fakeInner) AuthenticateWithClient(_ context.Context, _ string) (AuthResult, error) {
	f.authCalls.Add(1)
	return f.authResult, f.authErr
}

func (f *fakeInner) PullEntries(_ context.Context, _ int64, _ string, _ int64) ([]Entry, int64, error) {
	f.pullCalls.Add(1)
	return f.entries, f.pullLatest, nil
}

func (f *fakeInner) PushEntries(_ context.Context, _ int64, _ string, _ []Entry) ([]Entry, int64, error) {
	return nil, f.pushLatest, nil
}

func (f *fakeInner) RevokeToken(_ context.Context, _ string) error { return nil }

func (f *fakeInner) SetClientStatus(_ context.Context, _ int64, _, _ string) error { return nil }

func (f *fakeInner) SetHistoryRetain(n int) { f.retainN = n }

func (f *fakeInner) TouchClient(_ context.Context, clientID int64) {
	f.touchMu.Lock()
	defer f.touchMu.Unlock()
	f.touchCalls++
	f.touchedIDs = append(f.touchedIDs, clientID)
}

// newTestCache 构造 decorator 并注入可拨动的假时钟
func newTestCache(inner Store) (*cachedStore, *int64) {
	c := WithCache(inner)
	clock := int64(0)
	c.now = func() time.Time { return time.Unix(atomic.LoadInt64(&clock), 0) }
	return c, &clock
}

func advanceClock(clock *int64, seconds int64) { atomic.AddInt64(clock, seconds) }

func TestCacheAuthHitDoesNotCallInner(t *testing.T) {
	inner := &fakeInner{authResult: AuthResult{UserID: 7, ClientID: 3, ClientStatus: ClientStatusActive}}
	c, clock := newTestCache(inner)

	for i := 0; i < 5; i++ {
		res, err := c.AuthenticateWithClient(context.Background(), "tok")
		if err != nil || res != inner.authResult {
			t.Fatalf("call %d: res=%+v err=%v", i, res, err)
		}
	}
	if got := inner.authCalls.Load(); got != 1 {
		t.Fatalf("inner auth calls = %d, want 1（命中不得回库）", got)
	}
	// 换 token 是不同缓存键
	if _, err := c.AuthenticateWithClient(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
	if got := inner.authCalls.Load(); got != 2 {
		t.Fatalf("inner auth calls after second token = %d, want 2", got)
	}
	_ = clock
}

func TestCacheAuthErrNotCached(t *testing.T) {
	inner := &fakeInner{authErr: ErrNotFound}
	c, _ := newTestCache(inner)
	for i := 0; i < 3; i++ {
		if _, err := c.AuthenticateWithClient(context.Background(), "tok"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("call %d err = %v", i, err)
		}
	}
	if got := inner.authCalls.Load(); got != 3 {
		t.Fatalf("inner auth calls = %d, want 3（失败不做负缓存）", got)
	}
}

func TestCacheAuthExpiryFallsBackToInner(t *testing.T) {
	inner := &fakeInner{authResult: AuthResult{UserID: 1}}
	c, clock := newTestCache(inner)
	c.authTTL = 30 * time.Second

	_, err := c.AuthenticateWithClient(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	advanceClock(clock, 29)
	if _, err := c.AuthenticateWithClient(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
	if got := inner.authCalls.Load(); got != 1 {
		t.Fatalf("before TTL inner calls = %d, want 1", got)
	}
	advanceClock(clock, 2) // 越过 30s TTL
	if _, err := c.AuthenticateWithClient(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
	if got := inner.authCalls.Load(); got != 2 {
		t.Fatalf("after TTL inner calls = %d, want 2（TTL 兜底回库）", got)
	}
}

func TestCacheClearAuth(t *testing.T) {
	inner := &fakeInner{authResult: AuthResult{UserID: 1}}
	c, _ := newTestCache(inner)
	_, err := c.AuthenticateWithClient(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	c.ClearAuth()
	if _, err := c.AuthenticateWithClient(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
	if got := inner.authCalls.Load(); got != 2 {
		t.Fatalf("inner auth calls = %d, want 2（清空后回库）", got)
	}
}

func TestCacheRevokeAndBlockSelfClear(t *testing.T) {
	inner := &fakeInner{authResult: AuthResult{UserID: 1, ClientID: 2}}
	c, _ := newTestCache(inner)
	ctx := context.Background()
	if _, err := c.AuthenticateWithClient(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := c.RevokeToken(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AuthenticateWithClient(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	if got := inner.authCalls.Load(); got != 2 {
		t.Fatalf("after revoke inner calls = %d, want 2（revoke 自清）", got)
	}
	if err := c.SetClientStatus(ctx, -1, "laptop", ClientStatusBlocked); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AuthenticateWithClient(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	if got := inner.authCalls.Load(); got != 3 {
		t.Fatalf("after block inner calls = %d, want 3（block 自清）", got)
	}
}

func TestCachePullShortcutAndPushRefresh(t *testing.T) {
	inner := &fakeInner{entries: []Entry{{Kind: "env", Grp: "g", Key: "k", Revision: 5}}, pullLatest: 5}
	c, clock := newTestCache(inner)
	ctx := context.Background()

	// 首次 pull 回库并缓存 seq=5
	entries, latest, err := c.PullEntries(ctx, 1, "main", 0)
	if err != nil || latest != 5 || len(entries) != 1 {
		t.Fatalf("first pull = (%d entries, latest %d, err %v)", len(entries), latest, err)
	}
	// since=5 已最新：快捷判定，不回库
	entries, latest, err = c.PullEntries(ctx, 1, "main", 5)
	if err != nil || latest != 5 || len(entries) != 0 {
		t.Fatalf("up-to-date pull = (%d entries, latest %d, err %v)", len(entries), latest, err)
	}
	if got := inner.pullCalls.Load(); got != 1 {
		t.Fatalf("inner pull calls = %d, want 1（已最新快捷判定不回库）", got)
	}
	// since=3 落后于缓存：退回内层完整查询（方向性安全）
	if _, _, err = c.PullEntries(ctx, 1, "main", 3); err != nil {
		t.Fatal(err)
	}
	if got := inner.pullCalls.Load(); got != 2 {
		t.Fatalf("inner pull calls after lagging since = %d, want 2", got)
	}
	// push 推进 seq：缓存即时更新，pull 立即可用新快捷判定
	inner.pushLatest = 6
	if _, _, err = c.PushEntries(ctx, 1, "main", nil); err != nil {
		t.Fatal(err)
	}
	entries, latest, err = c.PullEntries(ctx, 1, "main", 6)
	if err != nil || latest != 6 || len(entries) != 0 {
		t.Fatalf("post-push pull = (%d entries, latest %d, err %v)", len(entries), latest, err)
	}
	if got := inner.pullCalls.Load(); got != 2 {
		t.Fatalf("inner pull calls after push = %d, want 2（push 后缓存即时可用）", got)
	}
	// TTL 过期后快捷判定失效，回库
	inner.pullLatest = 6
	advanceClock(clock, int64((DefaultVaultTTL+time.Minute)/time.Second))
	if _, _, err = c.PullEntries(ctx, 1, "main", 6); err != nil {
		t.Fatal(err)
	}
	if got := inner.pullCalls.Load(); got != 3 {
		t.Fatalf("inner pull calls after TTL = %d, want 3", got)
	}
}

func TestCacheVaultCapResets(t *testing.T) {
	inner := &fakeInner{pullLatest: 1}
	c, _ := newTestCache(inner)
	ctx := context.Background()
	// 灌满容量（不同 userID 即不同键），再塞一个触发重置
	for id := int64(0); id <= MaxCachedVaults; id++ {
		if _, _, err := c.PullEntries(ctx, id, "main", 0); err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	size := len(c.vaultSeq)
	c.mu.Unlock()
	if size >= MaxCachedVaults {
		t.Fatalf("vault cache size = %d, want < %d（到顶后应整表重置）", size, MaxCachedVaults)
	}
	// 重置后功能不受影响
	if _, _, err := c.PullEntries(ctx, 1, "main", 0); err != nil {
		t.Fatal(err)
	}
}

func TestCachePassThroughSetHistoryRetain(t *testing.T) {
	inner := &fakeInner{}
	c, _ := newTestCache(inner)
	c.SetHistoryRetain(7)
	if inner.retainN != 7 {
		t.Fatalf("inner retain = %d, want 7（decorator 必须透传）", inner.retainN)
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	inner := &fakeInner{authResult: AuthResult{UserID: 1}, pullLatest: 2}
	c, _ := newTestCache(inner)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = c.AuthenticateWithClient(ctx, "tok")
				_, _, _ = c.PullEntries(ctx, 1, "main", 2)
				_, _, _ = c.PushEntries(ctx, 1, "main", nil)
			}
		}()
	}
	wg.Wait()
}

func TestTouchClientBufferedAndFlushed(t *testing.T) {
	inner := &fakeInner{}
	c, _ := newTestCache(inner)
	ctx := context.Background()

	// N 次同 client touch 全部进缓冲，零落库
	for i := 0; i < 10; i++ {
		c.TouchClient(ctx, 42)
	}
	inner.touchMu.Lock()
	calls := inner.touchCalls
	inner.touchMu.Unlock()
	if calls != 0 {
		t.Fatalf("touches before flush = %d, want 0", calls)
	}
	// flush 落库一次（同 client 去重）
	c.FlushTouches(ctx)
	inner.touchMu.Lock()
	calls, ids := inner.touchCalls, append([]int64(nil), inner.touchedIDs...)
	inner.touchMu.Unlock()
	if calls != 1 || len(ids) != 1 || ids[0] != 42 {
		t.Fatalf("after flush = %d calls %v, want 1 call [42]", calls, ids)
	}
	// 缓冲已清空：再次 flush 零落库；flush 期间新 touch 归下一轮
	c.FlushTouches(ctx)
	c.TouchClient(ctx, 1)
	c.TouchClient(ctx, 2)
	c.TouchClient(ctx, 1) // 重复去重
	c.FlushTouches(ctx)
	inner.touchMu.Lock()
	calls, ids = inner.touchCalls, append([]int64(nil), inner.touchedIDs...)
	inner.touchMu.Unlock()
	if calls != 3 || len(ids) != 3 {
		t.Fatalf("multi-client flush = %d calls %v, want 3 calls", calls, ids)
	}
}

func TestTouchFlusherLoop(t *testing.T) {
	inner := &fakeInner{}
	c, _ := newTestCache(inner)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go c.StartTouchFlusher(ctx, 10*time.Millisecond)
	c.TouchClient(ctx, 7)
	deadline := time.Now().Add(2 * time.Second)
	for {
		inner.touchMu.Lock()
		calls := inner.touchCalls
		inner.touchMu.Unlock()
		if calls >= 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("flusher loop 未在周期内落库")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTouchClientIgnoresInvalidID(t *testing.T) {
	inner := &fakeInner{}
	c, _ := newTestCache(inner)
	c.TouchClient(context.Background(), 0)
	c.FlushTouches(context.Background())
	inner.touchMu.Lock()
	calls := inner.touchCalls
	inner.touchMu.Unlock()
	if calls != 0 {
		t.Fatalf("calls = %d, want 0（非法 id 不进缓冲）", calls)
	}
}
