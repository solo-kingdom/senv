// Package store 负责 senv-server 的全部 SQL 访问。
// 所有条目内容都是不透明密文字节，server 保持零知识。
package store

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/syncschema"
)

// 推送批量与单条大小上限（见 server-api spec：单条密文不超过 512KB）
const (
	MaxBatchEntries     = 1000
	MaxEntryCiphertext  = 512 * 1024
	MaxMetadataBlobSize = 64 * 1024
)

// MaxSkillCiphertext 是 kind=skill 条目（加密 tar 的 base64 密文）的单条上限：
// 明文 1MiB 经 AEAD + base64 后约 1.4MiB，留出余量取 1.5MiB。其它 kind 不变。
const MaxSkillCiphertext = 1536 * 1024

// 条目标识字段长度上限：server 侧独立防御，防止超长标识膨胀索引、日志与
// 请求体。客户端的字符集校验（internal/storage/validate.go）与此互补。
const (
	MaxKindLen = 32
	MaxGrpLen  = 128
	MaxKeyLen  = 256
)

// MaxVaultNameLen 是 vault 名字节数上限（与 grp 同量级）。
const MaxVaultNameLen = 128

// validateVaultName 在 server 侧独立校验 vault 名：可移植路径段规则 + 长度
// 上限。写路径会按名自动建 vault，无校验时认证用户可用任意/超长名称无限
// 创建 vault，膨胀存储与访问日志。
func validateVaultName(name string) error {
	if len(name) == 0 {
		return validationErrorf("vault 名不能为空")
	}
	if len(name) > MaxVaultNameLen {
		return validationErrorf("vault 名长度超过上限 %d 字节", MaxVaultNameLen)
	}
	if err := securefs.ValidateSegment(name); err != nil {
		return validationErrorf("vault 名无效：不能为空、含路径分隔符或非可移植字符")
	}
	return nil
}

// ErrNotFound 表示请求的用户/vault/条目不存在（HTTP 层映射为 404）
var ErrNotFound = errors.New("not found")

// ValidationError 表示请求本身不合法（缺参、超限、格式错误）。HTTP 层把它
// 映射为 400 并把原因透传给客户端——这类信息是客户端可理解的，与内部错误
// （一律 500 通用消息，细节仅进服务端日志）严格区分。
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func validationErrorf(format string, args ...any) *ValidationError {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Conflict 描述一次乐观锁冲突：条目在 server 端的当前 revision
type Conflict struct {
	Kind             string    `json:"kind"`
	Grp              string    `json:"grp"`
	Key              string    `json:"key"`
	CurrentRevision  int64     `json:"current_revision"`
	Deleted          bool      `json:"deleted"`
	Size             int64     `json:"size"`
	UpdatedAt        time.Time `json:"updated_at"`
	CiphertextSHA256 string    `json:"ciphertext_sha256,omitempty"`
}

// ConflictError 整批推送因乐观锁冲突被拒绝；绝不部分写入
type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("revision conflict on %d entries", len(e.Conflicts))
}

// Entry 是一条同步条目。ciphertext 对 server 完全不透明。
type Entry struct {
	Kind             string    `json:"kind"`
	Grp              string    `json:"grp"`
	Key              string    `json:"key"`
	Ciphertext       []byte    `json:"ciphertext,omitempty"`
	CiphertextSHA256 string    `json:"ciphertext_sha256,omitempty"`
	Size             int64     `json:"size,omitempty"`
	BaseRevision     int64     `json:"base_revision,omitempty"` // 推送时携带
	Revision         int64     `json:"revision"`                // 拉取/响应时携带
	Deleted          bool      `json:"deleted"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"` // server 端最近写入时间
}

// EntryKey 指定一条需要按需查询的条目标识。
type EntryKey struct {
	Kind string `json:"kind"`
	Grp  string `json:"grp"`
	Key  string `json:"key"`
}

// PullOptions 控制分页与全量 tombstone 过滤。
type PullOptions struct {
	Limit          int
	MaxBytes       int64
	IncludeDeleted bool
}

// PullPage 是一页增量拉取结果。
type PullPage struct {
	Entries        []Entry
	LatestRevision int64
	HasMore        bool
	NextSince      int64
	MetadataHash   string
}

// Store 是 senv-server 存储层的抽象：handler 与后台任务面向该接口工作，
// 具体实现为 pgStore（PostgreSQL）；缓存等 decorator 在该接缝上包装 inner。
// 除 Pool() 外全部方法可由任何实现安全提供。
type Store interface {
	// 生命周期
	Close()
	// Pool 返回底层连接池；仅 serve 启动预检等启动期入口使用，
	// 请求路径禁止绕过接口直接触碰连接池。
	Pool() *pgxpool.Pool

	// 用户与 token
	CreateUser(ctx context.Context, name string) (string, error)
	RevokeToken(ctx context.Context, token string) error
	Authenticate(ctx context.Context, token string) (int64, error)
	AuthenticateWithClient(ctx context.Context, token string) (AuthResult, error)

	// vault metadata
	GetMetadata(ctx context.Context, userID int64, vault string) ([]byte, error)
	PutMetadata(ctx context.Context, userID int64, vault string, blob []byte, baseHash string) error
	GetMetadataHistory(ctx context.Context, userID int64, vault string, limit int) ([]MetadataVersion, error)

	// 同步
	PushEntries(ctx context.Context, userID, clientID int64, vault string, entries []Entry) ([]Entry, int64, error)
	PullEntries(ctx context.Context, userID int64, vault string, since int64) ([]Entry, int64, error)
	PullEntriesPage(ctx context.Context, userID int64, vault string, since int64, opts PullOptions) (PullPage, error)
	LookupEntries(ctx context.Context, userID int64, vault string, keys []EntryKey, metaOnly bool) ([]Entry, int64, error)
	// SetMaxTombstonesPerPush 设置单批推送允许的最大删除条数；<=0 表示不限制
	SetMaxTombstonesPerPush(n int)

	// client 设备
	CreateRegistrationCode(ctx context.Context, userID int64, ttl time.Duration) (string, error)
	RegisterClient(ctx context.Context, code, name string) (string, *Client, error)
	SetClientStatus(ctx context.Context, userID int64, name, status string) error
	ListClients(ctx context.Context, userID int64) ([]Client, error)
	UserIDByName(ctx context.Context, name string) (int64, error)
	// UserIDByToken 按 token 解析归属用户（吊销审计用；token 行吊销后仍保留，可事后查询）
	UserIDByToken(ctx context.Context, token string) (int64, error)
	TouchClient(ctx context.Context, clientID int64)

	// 访问日志
	RecordAccess(ctx context.Context, e AccessEvent) error
	ListAccessLogs(ctx context.Context, f AccessLogFilter) ([]AccessEventRow, error)
	PruneAccessLogs(ctx context.Context, before time.Time) (int64, error)

	// 条目历史
	SetHistoryRetain(n int)
	// SetHistoryPrune 控制是否在 push 事务内裁剪；false 时由 admin cron 裁剪
	SetHistoryPrune(prune bool)
	// PruneAllHistory 把所有 vault 的条目历史裁剪到最近 retain 版（admin 用）
	PruneAllHistory(ctx context.Context, retain int) (int64, error)
	ListHistory(ctx context.Context, userID int64, vault string, f HistoryFilter) ([]HistoryVersion, error)
}

// pgStore 是 Store 的 PostgreSQL 实现，封装连接池与全部 SQL 访问
type pgStore struct {
	pool          *pgxpool.Pool
	historyRetain int
	// historyPrune 为 true 时在 push 事务内裁剪历史（默认，兼容旧行为）；
	// false 时交给 admin cron，历史对 serve 角色成为纯追加表。
	historyPrune bool
	// maxTombstones 是单批推送允许的最大删除条数（0 = 不限制）。
	// 防「合法但已失陷的 client」一次抹掉大量条目；默认关闭，部署侧按需启用。
	maxTombstones int

	// tokenPepper 配置后 token 存储哈希为 HMAC-SHA256(pepper, token)；
	// 空 = 原 SHA-256 行为。只驻留进程内存，绝不入库或入日志。
	tokenPepper []byte
	// legacyOK 旧 SHA-256 token 回退比对的正缓存（哈希 -> 窗口过期时刻），
	// 命中后窗口内免回退查询，防 pepper 启用后认证路径放大 DB 查询
	mu       sync.Mutex
	legacyOK map[string]time.Time
}

// legacyFallbackWindow 旧 SHA-256 token 回退比对的进程内正缓存窗口：
// 命中后该窗口内不再做回退查询（不放大 DB 查询），同时保持重复认证语义
const legacyFallbackWindow = time.Minute

// NewSQL 创建 pgStore（条目历史默认保留 DefaultHistoryRetain 版）
func NewSQL(pool *pgxpool.Pool) *pgStore {
	return &pgStore{pool: pool, historyRetain: DefaultHistoryRetain, historyPrune: true, legacyOK: map[string]time.Time{}}
}

// SetTokenPepper 配置 token 哈希 pepper（HMAC-SHA256 的 key）。空/未调用
// 保持原 SHA-256 行为不变。pepper 只驻留进程内存，绝不入库或入日志。
func (s *pgStore) SetTokenPepper(pepper []byte) {
	s.tokenPepper = pepper
}

// SetMaxTombstonesPerPush 设置单批推送允许的最大删除条数；<=0 不限制。
func (s *pgStore) SetMaxTombstonesPerPush(n int) {
	if n < 0 {
		n = 0
	}
	s.maxTombstones = n
}

// hashTokenPeppered 按当前配置计算 token 的存储哈希：配置 pepper 时为
// HMAC-SHA256(pepper, token)，否则为原 SHA-256。注册码等短生命周期凭证
// 仍用裸 hashToken（见 clients.go）
func (s *pgStore) hashTokenPeppered(token string) []byte {
	if len(s.tokenPepper) == 0 {
		return hashToken(token)
	}
	mac := hmac.New(sha256.New, s.tokenPepper)
	mac.Write([]byte(token))
	return mac.Sum(nil)
}

// legacyFallbackHash 回退比对键（裸 SHA-256）：正缓存与回退查询共用
func (s *pgStore) legacyFallbackHit(legacyHash []byte) bool {
	if len(s.tokenPepper) == 0 {
		return false
	}
	key := string(legacyHash)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.legacyOK[key]; ok && time.Now().Before(t) {
		return true
	}
	delete(s.legacyOK, key)
	return false
}

// markLegacyFallback 记录一次回退比对命中（正缓存一个窗口）
func (s *pgStore) markLegacyFallback(legacyHash []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.legacyOK[string(legacyHash)] = time.Now().Add(legacyFallbackWindow)
}

// New 是返回接口的兼容构造入口，等价于 NewSQL
func New(pool *pgxpool.Pool) Store {
	return NewSQL(pool)
}

// Close 关闭连接池
func (s *pgStore) Close() {
	s.pool.Close()
}

// Pool 返回底层连接池（admin/migrate 入口使用）
func (s *pgStore) Pool() *pgxpool.Pool {
	return s.pool
}

// --- 用户与 token ---

// GenerateToken 生成 32 字节随机 token（base64url），明文只在创建时展示一次
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken 对 token 做 SHA-256；库中只存哈希，不可反推明文
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateUser 创建用户并签发 token，返回一次性明文 token
func (s *pgStore) CreateUser(ctx context.Context, name string) (string, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var userID int64
	err = tx.QueryRow(ctx, `INSERT INTO users (name) VALUES ($1) RETURNING id`, name).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("创建用户失败: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tokens (user_id, token_hash) VALUES ($1, $2)`, userID, s.hashTokenPeppered(token)); err != nil {
		return "", fmt.Errorf("签发 token 失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return token, nil
}

// RevokeToken 吊销指定 token（按哈希匹配），不影响同用户其他 token
func (s *pgStore) RevokeToken(ctx context.Context, token string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE tokens SET revoked_at = $1 WHERE token_hash = $2 AND revoked_at IS NULL`,
		time.Now(), s.hashTokenPeppered(token))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 && len(s.tokenPepper) > 0 {
		// 旧 SHA-256 时代的 token：回退比对一次（吊销同样要兼容存量）
		tag, err = s.pool.Exec(ctx, `UPDATE tokens SET revoked_at = $1 WHERE token_hash = $2 AND revoked_at IS NULL`,
			time.Now(), hashToken(token))
		if err != nil {
			return err
		}
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("token 不存在或已吊销")
	}
	// 吊销生效依赖认证缓存同步失效：admin 进程与 serve 进程隔离，
	// 经 pg_notify 广播（best-effort，丢失由消费端 TTL 兜底）
	s.notifyCacheInvalidation(ctx)
	return nil
}

// Authenticate 用 token 换取 user_id；无效或已吊销返回 ErrNotFound（不泄露存在性）。
// 配置 pepper 后对存量 SHA-256 哈希做一次回退比对（正缓存窗口内不重复查询），
// 回退命中记慢日志提示轮换，响应语义与主路径完全一致。
func (s *pgStore) Authenticate(ctx context.Context, token string) (int64, error) {
	var userID int64
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM tokens WHERE token_hash = $1 AND revoked_at IS NULL`, s.hashTokenPeppered(token)).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	legacy := hashToken(token)
	if s.legacyFallbackHit(legacy) {
		return s.queryUserByTokenHash(ctx, legacy)
	}
	if len(s.tokenPepper) == 0 {
		return 0, ErrNotFound
	}
	userID, err = s.queryUserByTokenHash(ctx, legacy)
	if err == nil {
		s.markLegacyFallback(legacy)
		slog.Warn("legacy sha256 token hash used, please rotate the token",
			"user_id", userID)
	}
	return userID, err
}

// queryUserByTokenHash 按存储哈希查 token 归属用户（回退比对共用）
func (s *pgStore) queryUserByTokenHash(ctx context.Context, tokenHash []byte) (int64, error) {
	var userID int64
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM tokens WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return userID, nil
}

// --- vault metadata ---

// ensureVault 取 vault id，不存在则为该用户创建（vault 归属用户，天然隔离）
func ensureVault(ctx context.Context, tx pgx.Tx, userID int64, name string) (int64, error) {
	var vaultID int64
	err := tx.QueryRow(ctx,
		`INSERT INTO vaults (user_id, name) VALUES ($1, $2)
		 ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`, userID, name).Scan(&vaultID)
	return vaultID, err
}

// lookupVault 只查不建；跨用户访问返回 ErrNotFound（不泄露 vault 存在性）
func lookupVault(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID int64, name string) (int64, error) {
	var vaultID int64
	err := db.QueryRow(ctx, `SELECT id FROM vaults WHERE user_id = $1 AND name = $2`, userID, name).Scan(&vaultID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return vaultID, nil
}

// GetMetadata 读取 vault metadata blob（原样透传，不解析）
func (s *pgStore) GetMetadata(ctx context.Context, userID int64, vault string) ([]byte, error) {
	if err := validateVaultName(vault); err != nil {
		return nil, err
	}
	vaultID, err := lookupVault(ctx, s.pool, userID, vault)
	if err != nil {
		return nil, err
	}
	var blob []byte
	err = s.pool.QueryRow(ctx, `SELECT blob FROM vault_metadata WHERE vault_id = $1`, vaultID).Scan(&blob)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return blob, nil
}

// PutMetadata 写入 vault metadata blob（vault 不存在时自动创建）。
// baseHash 非空时做乐观锁：与当前 blob 的 sha256 不一致返回 MetadataConflictError；
// 覆盖前把上一版写入 vault_metadata_history（保留 historyRetain 版）；内容未变化时
// 不写前像、不更新时间（重复推送不消耗历史）。
func (s *pgStore) PutMetadata(ctx context.Context, userID int64, vault string, blob []byte, baseHash string) error {
	if err := validateVaultName(vault); err != nil {
		return err
	}
	if len(blob) == 0 {
		return validationErrorf("metadata blob 不能为空")
	}
	if len(blob) > MaxMetadataBlobSize {
		return validationErrorf("metadata blob 超过大小上限 %d 字节", MaxMetadataBlobSize)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	vaultID, err := ensureVault(ctx, tx, userID, vault)
	if err != nil {
		return err
	}
	var current []byte
	err = tx.QueryRow(ctx, `SELECT blob FROM vault_metadata WHERE vault_id = $1 FOR UPDATE`, vaultID).Scan(&current)
	switch {
	case err == nil:
		if baseHash != "" && metadataHash(current) != baseHash {
			return &MetadataConflictError{}
		}
		if bytes.Equal(current, blob) {
			// 内容未变化：不写前像、不推进更新时间
			return tx.Commit(ctx)
		}
		if s.historyRetain > 0 {
			if err := recordMetadataPreimage(ctx, tx, vaultID, s.historyRetain, s.historyPrune); err != nil {
				return err
			}
		}
	case errors.Is(err, pgx.ErrNoRows):
		// 首次写入：没有前像可留
	default:
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO vault_metadata (vault_id, blob, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (vault_id) DO UPDATE SET blob = EXCLUDED.blob, updated_at = EXCLUDED.updated_at`,
		vaultID, blob)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- 同步 API ---

// nextRevision 推进 vault 级单调序列并返回新值（行锁保证并发下不重复）
func nextRevision(ctx context.Context, tx pgx.Tx, vaultID int64) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `UPDATE vaults SET seq = seq + 1 WHERE id = $1 RETURNING seq`, vaultID).Scan(&seq)
	return seq, err
}

// maxCiphertextFor 返回 kind 对应的单条密文上限；仅 skill 放宽，其余沿用默认。
func maxCiphertextFor(kind string) int {
	if kind == syncschema.KindSkill {
		return MaxSkillCiphertext
	}
	return MaxEntryCiphertext
}

// validateEntry 校验单条推送条目的标识字段与大小，返回客户端可理解的错误
func validateEntry(e Entry) *ValidationError {
	if len(e.Kind) == 0 {
		return validationErrorf("条目缺少 kind")
	}
	if len(e.Kind) > MaxKindLen {
		return validationErrorf("条目 kind 长度超过上限 %d 字节", MaxKindLen)
	}
	if len(e.Grp) > MaxGrpLen {
		return validationErrorf("条目 grp 长度超过上限 %d 字节", MaxGrpLen)
	}
	if len(e.Key) > MaxKeyLen {
		return validationErrorf("条目 key 长度超过上限 %d 字节", MaxKeyLen)
	}
	if err := syncschema.ValidateIdentity(e.Kind, e.Grp, e.Key); err != nil {
		return validationErrorf("条目标识无效: %s", err)
	}
	if limit := maxCiphertextFor(e.Kind); !e.Deleted && len(e.Ciphertext) > limit {
		return validationErrorf("条目 %s/%s/%s 密文超过大小上限 %d 字节", e.Kind, e.Grp, e.Key, limit)
	}
	return nil
}

// PushEntries 乐观锁批量推送：整批一个事务，任一冲突则整批拒绝。
// clientID/userID 由调用方从已认证 token 推出，写入历史行作为操作者归因。
func (s *pgStore) PushEntries(ctx context.Context, userID, clientID int64, vault string, entries []Entry) ([]Entry, int64, error) {
	if err := validateVaultName(vault); err != nil {
		return nil, 0, err
	}
	if len(entries) == 0 {
		return nil, 0, validationErrorf("推送批次不能为空")
	}
	if len(entries) > MaxBatchEntries {
		return nil, 0, validationErrorf("单批条目数超过上限 %d", MaxBatchEntries)
	}
	for _, e := range entries {
		if verr := validateEntry(e); verr != nil {
			return nil, 0, verr
		}
	}
	if s.maxTombstones > 0 {
		deleted := 0
		for _, e := range entries {
			if e.Deleted {
				deleted++
			}
		}
		if deleted > s.maxTombstones {
			return nil, 0, validationErrorf("单批删除条目数 %d 超过上限 %d（--max-tombstones-per-push）", deleted, s.maxTombstones)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	// 写路径才创建 vault；拉取路径只读不建
	vaultID, err := ensureVault(ctx, tx, userID, vault)
	if err != nil {
		return nil, 0, err
	}

	// 整批先做重复 key 检查与 base_revision 校验，收集冲突清单；冲突则整批回滚。
	type entryTuple struct{ kind, grp, key string }
	seen := make(map[entryTuple]struct{}, len(entries))
	kinds := make([]string, len(entries))
	grps := make([]string, len(entries))
	keys := make([]string, len(entries))
	for i, e := range entries {
		t := entryTuple{e.Kind, e.Grp, e.Key}
		if _, dup := seen[t]; dup {
			return nil, 0, validationErrorf("推送批次包含重复条目 %s/%s/%s", e.Kind, e.Grp, e.Key)
		}
		seen[t] = struct{}{}
		kinds[i] = e.Kind
		grps[i] = e.Grp
		keys[i] = e.Key
	}

	existing := make(map[entryTuple]Conflict, len(entries))
	rows, err := tx.Query(ctx,
		`SELECT e.kind, e.grp, e.key, e.revision, e.deleted, e.ciphertext, e.updated_at
		 FROM entries e
		 JOIN unnest($2::text[], $3::text[], $4::text[]) AS u(kind, grp, key)
		   ON e.kind = u.kind AND e.grp = u.grp AND e.key = u.key
		 WHERE e.vault_id = $1`,
		vaultID, kinds, grps, keys)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var c Conflict
		var ct []byte
		if err := rows.Scan(&c.Kind, &c.Grp, &c.Key, &c.CurrentRevision, &c.Deleted, &ct, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		c.Size = int64(len(ct))
		if !c.Deleted && len(ct) > 0 {
			sum := sha256.Sum256(ct)
			c.CiphertextSHA256 = hex.EncodeToString(sum[:])
		}
		existing[entryTuple{c.Kind, c.Grp, c.Key}] = c
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()

	var conflicts []Conflict
	for _, e := range entries {
		cur, ok := existing[entryTuple{e.Kind, e.Grp, e.Key}]
		if !ok {
			if e.BaseRevision != 0 {
				conflicts = append(conflicts, Conflict{
					Kind: e.Kind, Grp: e.Grp, Key: e.Key,
					CurrentRevision: 0, Deleted: true,
				})
			}
			continue
		}
		if e.BaseRevision != cur.CurrentRevision {
			conflicts = append(conflicts, cur)
		}
	}
	if len(conflicts) > 0 {
		return nil, 0, &ConflictError{Conflicts: conflicts}
	}

	// 无冲突：历史前像留存 + 单次预分配连续 revision 号段 + 批量 upsert。
	if s.historyRetain > 0 {
		if err := recordHistoryPreimages(ctx, tx, vaultID, entries, userID, clientID); err != nil {
			return nil, 0, err
		}
	}
	var latest int64
	if err := tx.QueryRow(ctx,
		`UPDATE vaults SET seq = seq + $1 WHERE id = $2 RETURNING seq`,
		int64(len(entries)), vaultID).Scan(&latest); err != nil {
		return nil, 0, err
	}
	startRev := latest - int64(len(entries)) + 1
	cts := make([][]byte, len(entries))
	revs := make([]int64, len(entries))
	dels := make([]bool, len(entries))
	for i := range entries {
		rev := startRev + int64(i)
		entries[i].Revision = rev
		revs[i] = rev
		dels[i] = entries[i].Deleted
		if !entries[i].Deleted {
			cts[i] = entries[i].Ciphertext
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO entries (vault_id, kind, grp, key, ciphertext, revision, deleted, updated_at)
		 SELECT $1, u.kind, u.grp, u.key,
		        CASE WHEN u.deleted THEN NULL ELSE u.ct END,
		        u.rev, u.deleted, now()
		 FROM unnest($2::text[], $3::text[], $4::text[], $5::bytea[], $6::bigint[], $7::boolean[])
		      AS u(kind, grp, key, ct, rev, deleted)
		 ON CONFLICT (vault_id, kind, grp, key)
		 DO UPDATE SET ciphertext = EXCLUDED.ciphertext,
		               revision   = EXCLUDED.revision,
		               deleted    = EXCLUDED.deleted,
		               updated_at = EXCLUDED.updated_at`,
		vaultID, kinds, grps, keys, cts, revs, dels); err != nil {
		return nil, 0, err
	}
	if s.historyRetain > 0 && s.historyPrune {
		if err := pruneHistory(ctx, tx, vaultID, s.historyRetain, entries); err != nil {
			return nil, 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return entries, latest, nil
}

// PullEntries 增量拉取：返回 revision > since 的全部条目（含删除标记）与最新 revision。
// 空增量返回空列表与当前最新 revision，不报错。
func (s *pgStore) PullEntries(ctx context.Context, userID int64, vault string, since int64) ([]Entry, int64, error) {
	page, err := s.PullEntriesPage(ctx, userID, vault, since, PullOptions{IncludeDeleted: true})
	if err != nil {
		return nil, 0, err
	}
	return page.Entries, page.LatestRevision, nil
}

// PullEntriesPage 支持可选分页（Limit / MaxBytes）、全量跳过 tombstone 及返回 metadata_hash。
func (s *pgStore) PullEntriesPage(ctx context.Context, userID int64, vault string, since int64, opts PullOptions) (PullPage, error) {
	if err := validateVaultName(vault); err != nil {
		return PullPage{}, err
	}
	vaultID, err := lookupVault(ctx, s.pool, userID, vault)
	if err != nil {
		return PullPage{}, err
	}
	var latest int64
	if err := s.pool.QueryRow(ctx, `SELECT seq FROM vaults WHERE id = $1`, vaultID).Scan(&latest); err != nil {
		return PullPage{}, err
	}

	var metaHash string
	var metaBlob []byte
	if err := s.pool.QueryRow(ctx, `SELECT blob FROM vault_metadata WHERE vault_id = $1`, vaultID).Scan(&metaBlob); err == nil {
		sum := sha256.Sum256(metaBlob)
		metaHash = hex.EncodeToString(sum[:])
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PullPage{}, err
	}

	query := `SELECT kind, grp, key, ciphertext, revision, deleted, updated_at FROM entries
		 WHERE vault_id = $1 AND revision > $2`
	if since == 0 && !opts.IncludeDeleted {
		query += ` AND deleted = FALSE`
	}
	query += ` ORDER BY revision`
	args := []any{vaultID, since}
	if opts.Limit > 0 {
		query += ` LIMIT $3`
		args = append(args, opts.Limit+1)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return PullPage{}, err
	}
	defer rows.Close()

	entries := []Entry{}
	var totalBytes int64
	hasMore := false
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Kind, &e.Grp, &e.Key, &e.Ciphertext, &e.Revision, &e.Deleted, &e.UpdatedAt); err != nil {
			return PullPage{}, err
		}
		if opts.Limit > 0 && len(entries) >= opts.Limit {
			hasMore = true
			break
		}
		if opts.MaxBytes > 0 && len(entries) > 0 && totalBytes+int64(len(e.Ciphertext)) > opts.MaxBytes {
			hasMore = true
			break
		}
		totalBytes += int64(len(e.Ciphertext))
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return PullPage{}, err
	}
	var nextSince int64
	if hasMore && len(entries) > 0 {
		nextSince = entries[len(entries)-1].Revision
	}
	return PullPage{
		Entries:        entries,
		LatestRevision: latest,
		HasMore:        hasMore,
		NextSince:      nextSince,
		MetadataHash:   metaHash,
	}, nil
}

// LookupEntries 批量按 (kind, grp, key) 查询远端条目当前状态或密文。
func (s *pgStore) LookupEntries(ctx context.Context, userID int64, vault string, keys []EntryKey, metaOnly bool) ([]Entry, int64, error) {
	if err := validateVaultName(vault); err != nil {
		return nil, 0, err
	}
	if len(keys) > MaxBatchEntries {
		return nil, 0, validationErrorf("单次查询条目数超过上限 %d", MaxBatchEntries)
	}
	for _, k := range keys {
		if len(k.Kind) == 0 || len(k.Kind) > MaxKindLen || len(k.Grp) > MaxGrpLen || len(k.Key) > MaxKeyLen {
			return nil, 0, validationErrorf("条目标识长度无效")
		}
		if err := syncschema.ValidateIdentity(k.Kind, k.Grp, k.Key); err != nil {
			return nil, 0, validationErrorf("条目标识无效: %s", err)
		}
	}
	vaultID, err := lookupVault(ctx, s.pool, userID, vault)
	if err != nil {
		return nil, 0, err
	}
	var latest int64
	if err := s.pool.QueryRow(ctx, `SELECT seq FROM vaults WHERE id = $1`, vaultID).Scan(&latest); err != nil {
		return nil, 0, err
	}
	if len(keys) == 0 {
		return []Entry{}, latest, nil
	}

	kinds := make([]string, len(keys))
	grps := make([]string, len(keys))
	keyNames := make([]string, len(keys))
	for i, k := range keys {
		kinds[i] = k.Kind
		grps[i] = k.Grp
		keyNames[i] = k.Key
	}

	rows, err := s.pool.Query(ctx,
		`SELECT e.kind, e.grp, e.key, e.ciphertext, e.revision, e.deleted, e.updated_at
		 FROM entries e
		 JOIN unnest($2::text[], $3::text[], $4::text[]) AS q(kind, grp, key)
		   ON e.kind = q.kind AND e.grp = q.grp AND e.key = q.key
		 WHERE e.vault_id = $1`,
		vaultID, kinds, grps, keyNames)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		var e Entry
		var ct []byte
		if err := rows.Scan(&e.Kind, &e.Grp, &e.Key, &ct, &e.Revision, &e.Deleted, &e.UpdatedAt); err != nil {
			return nil, 0, err
		}
		e.Size = int64(len(ct))
		if !e.Deleted && len(ct) > 0 {
			sum := sha256.Sum256(ct)
			e.CiphertextSHA256 = hex.EncodeToString(sum[:])
		}
		if !metaOnly {
			e.Ciphertext = ct
		}
		entries = append(entries, e)
	}
	return entries, latest, rows.Err()
}
