// 进程内缓存 decorator：包装任意 Store 实现，缓存认证结果与 vault seq。
// 失效语义（driver server-storage-cache grill D8/D10，见 design）：
//   - 认证结果：本进程 revoke/block 经 decorator 自清；跨进程（admin 一次性
//     进程）经 pg LISTEN/NOTIFY 广播全清；TTL 仅作广播丢失的兜底上限。
//   - vault seq：vault 只增不删，无需失效；push 同进程即时更新；pull 侧
//     缓存偏旧只会退化为完整范围查询（方向性安全，不产生假「已最新」）。
package store

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 缓存参数：认证 TTL 是失效广播丢失时的兜底上限（server-auth spec：认证
// 缓存 TTL MUST NOT 超过 30 秒），不是性能旋钮；vault 条目容量上限只为
// 防病态膨胀（vault 数量级是个位数用户 × 个位 vault，实际到不了）。
const (
	DefaultAuthTTL  = 30 * time.Second
	DefaultVaultTTL = 5 * time.Minute
	MaxCachedVaults = 4096
)

// authKey 是 token 的 SHA-256 哈希（库中唯一持有的形态，不缓存明文）
type authKey [sha256.Size]byte

type authEntry struct {
	result  AuthResult
	expires time.Time
}

type vaultSeqKey struct {
	userID int64
	vault  string
}

type vaultSeqEntry struct {
	seq     int64
	expires time.Time
}

type cachedStore struct {
	inner Store
	// now 可注入时钟，供 TTL 过期测试
	now func() time.Time

	authTTL  time.Duration
	vaultTTL time.Duration

	mu       sync.Mutex
	auth     map[authKey]authEntry
	vaultSeq map[vaultSeqKey]vaultSeqEntry

	// last_seen 内存节流缓冲（touch 切片）：TouchClient 只记内存，由
	// StartTouchFlusher 周期或停机前 FlushTouches 批量落库。纯展示字段，
	// 崩溃丢弃缓冲无实害（driver grill D5）。
	touches map[int64]struct{}
}

// WithCache 在 inner 外包一层进程内缓存；返回具体类型以便调用方持有
// ClearAuth 交给失效广播监听。
func WithCache(inner Store) *cachedStore {
	return &cachedStore{
		inner:    inner,
		now:      time.Now,
		authTTL:  DefaultAuthTTL,
		vaultTTL: DefaultVaultTTL,
		auth:     map[authKey]authEntry{},
		vaultSeq: map[vaultSeqKey]vaultSeqEntry{},
		touches:  map[int64]struct{}{},
	}
}

// 编译期断言：接口增删方法时先于测试失败
var _ Store = (*cachedStore)(nil)

// ClearAuth 清空全部认证缓存。本进程 revoke/block 后由 decorator 自行调用；
// 跨进程的失效经 StartInvalidationListener 广播触发本方法。
func (c *cachedStore) ClearAuth() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.auth = map[authKey]authEntry{}
}

// Close 透传 inner
func (c *cachedStore) Close() { c.inner.Close() }

// Pool 透传 inner
func (c *cachedStore) Pool() *pgxpool.Pool { return c.inner.Pool() }

// CreateUser 透传（新 token 天然 cache miss）
func (c *cachedStore) CreateUser(ctx context.Context, name string) (string, error) {
	return c.inner.CreateUser(ctx, name)
}

// RevokeToken 透传并在成功后自清认证缓存：即使本 decorator 被用在
// admin 语义的调用方（当前拓扑不会），缓存也与库内事实保持一致
func (c *cachedStore) RevokeToken(ctx context.Context, token string) error {
	if err := c.inner.RevokeToken(ctx, token); err != nil {
		return err
	}
	c.ClearAuth()
	return nil
}

// Authenticate 透传：仅 AuthenticateWithClient 在认证热路径上，缓存它即可
func (c *cachedStore) Authenticate(ctx context.Context, token string) (int64, error) {
	return c.inner.Authenticate(ctx, token)
}

// AuthenticateWithClient 缓存认证结果（键为 token 哈希）。未命中回库；
// 认证失败不缓存——被吊销 token 不做负缓存，新签发 token 必须立即可用，
// 且无界负缓存条目是未认证流量的内存 DoS 面。
func (c *cachedStore) AuthenticateWithClient(ctx context.Context, token string) (AuthResult, error) {
	key := authKey(sha256.Sum256([]byte(token)))
	now := c.now()
	c.mu.Lock()
	e, ok := c.auth[key]
	c.mu.Unlock()
	if ok && now.Before(e.expires) {
		return e.result, nil
	}
	res, err := c.inner.AuthenticateWithClient(ctx, token)
	if err == nil {
		c.mu.Lock()
		c.auth[key] = authEntry{result: res, expires: now.Add(c.authTTL)}
		c.mu.Unlock()
	}
	return res, err
}

// GetMetadata 透传：metadata 低频，不值得缓存
func (c *cachedStore) GetMetadata(ctx context.Context, userID int64, vault string) ([]byte, error) {
	return c.inner.GetMetadata(ctx, userID, vault)
}

// PutMetadata 透传
func (c *cachedStore) PutMetadata(ctx context.Context, userID int64, vault string, blob []byte) error {
	return c.inner.PutMetadata(ctx, userID, vault, blob)
}

// PushEntries 透传，成功后把返回的最新 revision 写入 seq 缓存（同进程
// 写路径是 seq 唯一的推进来源，缓存因此始终不低于库内事实）
func (c *cachedStore) PushEntries(ctx context.Context, userID int64, vault string, entries []Entry) ([]Entry, int64, error) {
	out, latest, err := c.inner.PushEntries(ctx, userID, vault, entries)
	if err == nil {
		c.putVaultSeq(vaultSeqKey{userID, vault}, latest)
	}
	return out, latest, err
}

// PullEntries 带「已最新」快捷判定：since >= 缓存 seq 时直接返回空增量，
// 省掉 lookupVault + seq 读 + 范围查询共 3 次往返。seq 单调且仅本进程 push
// 推进，缓存偏旧（值更小）只会漏判「已最新」而退回内层完整查询，不会
// 假命中——方向性安全。
func (c *cachedStore) PullEntries(ctx context.Context, userID int64, vault string, since int64) ([]Entry, int64, error) {
	key := vaultSeqKey{userID, vault}
	c.mu.Lock()
	e, ok := c.vaultSeq[key]
	c.mu.Unlock()
	if ok && c.now().Before(e.expires) && since >= e.seq {
		return []Entry{}, e.seq, nil
	}
	entries, latest, err := c.inner.PullEntries(ctx, userID, vault, since)
	if err == nil {
		c.putVaultSeq(key, latest)
	}
	return entries, latest, err
}

// CreateRegistrationCode 透传（admin 语义，一次性进程）
func (c *cachedStore) CreateRegistrationCode(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	return c.inner.CreateRegistrationCode(ctx, userID, ttl)
}

// RegisterClient 透传（serve 语义：新 token 天然 cache miss）
func (c *cachedStore) RegisterClient(ctx context.Context, code, name string) (string, *Client, error) {
	return c.inner.RegisterClient(ctx, code, name)
}

// SetClientStatus 透传并在成功后自清认证缓存（同 RevokeToken 的自洽防线）
func (c *cachedStore) SetClientStatus(ctx context.Context, userID int64, name, status string) error {
	if err := c.inner.SetClientStatus(ctx, userID, name, status); err != nil {
		return err
	}
	c.ClearAuth()
	return nil
}

// ListClients 透传（admin 语义）
func (c *cachedStore) ListClients(ctx context.Context, userID int64) ([]Client, error) {
	return c.inner.ListClients(ctx, userID)
}

// UserIDByName 透传（admin 语义）
func (c *cachedStore) UserIDByName(ctx context.Context, name string) (int64, error) {
	return c.inner.UserIDByName(ctx, name)
}

// TouchClient 只记内存缓冲即返回：省掉每请求一次 UPDATE 往返。真实落库
// 由 StartTouchFlusher 周期批量执行（SQL 内 1 分钟谓词保留为写侧双保险）。
// best-effort：server 崩溃丢弃缓冲，last_seen 纯展示字段无安全语义。
func (c *cachedStore) TouchClient(_ context.Context, clientID int64) {
	if clientID <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.touches[clientID] = struct{}{}
}

// FlushTouches 把缓冲中的 client 批量落库并清空缓冲（停机链路与周期
// flusher 共用）。flush 期间新到的 touch 落入新缓冲，下一轮再落库。
func (c *cachedStore) FlushTouches(ctx context.Context) {
	c.mu.Lock()
	pending := c.touches
	c.touches = map[int64]struct{}{}
	c.mu.Unlock()
	for id := range pending {
		c.inner.TouchClient(ctx, id)
	}
}

// StartTouchFlusher 周期批量落库 last_seen 缓冲；阻塞至 ctx 取消，调用方
// 放入独立 goroutine。周期与既有 SQL 节流粒度（1 分钟）对齐。
func (c *cachedStore) StartTouchFlusher(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.FlushTouches(ctx)
		}
	}
}

// RecordAccess 透传：访问日志只写不读，不缓存
func (c *cachedStore) RecordAccess(ctx context.Context, e AccessEvent) error {
	return c.inner.RecordAccess(ctx, e)
}

// ListAccessLogs 透传（admin 语义）
func (c *cachedStore) ListAccessLogs(ctx context.Context, f AccessLogFilter) ([]AccessEventRow, error) {
	return c.inner.ListAccessLogs(ctx, f)
}

// PruneAccessLogs 透传
func (c *cachedStore) PruneAccessLogs(ctx context.Context, before time.Time) (int64, error) {
	return c.inner.PruneAccessLogs(ctx, before)
}

// SetHistoryRetain 透传
func (c *cachedStore) SetHistoryRetain(n int) { c.inner.SetHistoryRetain(n) }

// ListHistory 透传：低频只读路径，收益不值得缓存
func (c *cachedStore) ListHistory(ctx context.Context, userID int64, vault string, f HistoryFilter) ([]HistoryVersion, error) {
	return c.inner.ListHistory(ctx, userID, vault, f)
}

// putVaultSeq 写入 vault seq 缓存；容量到顶先清过期条目，仍满则整表重置
// （整清永远安全，容量上限不是性能旋钮）
func (c *cachedStore) putVaultSeq(key vaultSeqKey, seq int64) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.vaultSeq) >= MaxCachedVaults {
		for k, e := range c.vaultSeq {
			if !now.Before(e.expires) {
				delete(c.vaultSeq, k)
			}
		}
		if len(c.vaultSeq) >= MaxCachedVaults {
			c.vaultSeq = map[vaultSeqKey]vaultSeqEntry{}
		}
	}
	c.vaultSeq[key] = vaultSeqEntry{seq: seq, expires: now.Add(c.vaultTTL)}
}
