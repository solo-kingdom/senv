// Package store 负责 senv-server 的全部 SQL 访问。
// 所有条目内容都是不透明密文字节，server 保持零知识。
package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/syncschema"
	"sync"
)

// 推送批量与单条大小上限（见 server-api spec：单条密文不超过 512KB）
const (
	MaxBatchEntries     = 1000
	MaxEntryCiphertext  = 512 * 1024
	MaxMetadataBlobSize = 64 * 1024
)

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
	Kind            string    `json:"kind"`
	Grp             string    `json:"grp"`
	Key             string    `json:"key"`
	CurrentRevision int64     `json:"current_revision"`
	Deleted         bool      `json:"deleted"`
	Size            int64     `json:"size"`
	UpdatedAt       time.Time `json:"updated_at"`
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
	Kind         string    `json:"kind"`
	Grp          string    `json:"grp"`
	Key          string    `json:"key"`
	Ciphertext   []byte    `json:"ciphertext,omitempty"`
	BaseRevision int64     `json:"base_revision,omitempty"` // 推送时携带
	Revision     int64     `json:"revision"`                // 拉取/响应时携带
	Deleted      bool      `json:"deleted"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"` // server 端最近写入时间
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
	PutMetadata(ctx context.Context, userID int64, vault string, blob []byte) error

	// 同步
	PushEntries(ctx context.Context, userID int64, vault string, entries []Entry) ([]Entry, int64, error)
	PullEntries(ctx context.Context, userID int64, vault string, since int64) ([]Entry, int64, error)

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
	ListHistory(ctx context.Context, userID int64, vault string, f HistoryFilter) ([]HistoryVersion, error)
}

// pgStore 是 Store 的 PostgreSQL 实现，封装连接池与全部 SQL 访问
type pgStore struct {
	pool          *pgxpool.Pool
	historyRetain int

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
	return &pgStore{pool: pool, historyRetain: DefaultHistoryRetain, legacyOK: map[string]time.Time{}}
}

// SetTokenPepper 配置 token 哈希 pepper（HMAC-SHA256 的 key）。空/未调用
// 保持原 SHA-256 行为不变。pepper 只驻留进程内存，绝不入库或入日志。
func (s *pgStore) SetTokenPepper(pepper []byte) {
	s.tokenPepper = pepper
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

// PutMetadata 写入 vault metadata blob（vault 不存在时自动创建）
func (s *pgStore) PutMetadata(ctx context.Context, userID int64, vault string, blob []byte) error {
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
	if !e.Deleted && len(e.Ciphertext) > MaxEntryCiphertext {
		return validationErrorf("条目 %s/%s/%s 密文超过大小上限 %d 字节", e.Kind, e.Grp, e.Key, MaxEntryCiphertext)
	}
	return nil
}

// PushEntries 乐观锁批量推送：整批一个事务，任一冲突则整批拒绝
func (s *pgStore) PushEntries(ctx context.Context, userID int64, vault string, entries []Entry) ([]Entry, int64, error) {
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

	// 整批先做 base_revision 校验，收集冲突清单；冲突则整批回滚
	var conflicts []Conflict
	for _, e := range entries {
		var current Conflict
		err := tx.QueryRow(ctx,
			`SELECT revision, deleted, COALESCE(length(ciphertext), 0), updated_at
			 FROM entries WHERE vault_id = $1 AND kind = $2 AND grp = $3 AND key = $4`,
			vaultID, e.Kind, e.Grp, e.Key).Scan(
			&current.CurrentRevision, &current.Deleted, &current.Size, &current.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			// 新条目：base_revision 必须为 0
			if e.BaseRevision != 0 {
				conflicts = append(conflicts, Conflict{
					Kind: e.Kind, Grp: e.Grp, Key: e.Key,
					CurrentRevision: 0, Deleted: true,
				})
			}
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		if e.BaseRevision != current.CurrentRevision {
			current.Kind, current.Grp, current.Key = e.Kind, e.Grp, e.Key
			conflicts = append(conflicts, current)
		}
	}
	if len(conflicts) > 0 {
		return nil, 0, &ConflictError{Conflicts: conflicts}
	}

	// 无冲突：逐条写入，每条取新的单调 revision（更新与删除都推进）。
	// 历史留存（可由 --history-retain<=0 关闭）：先把受影响条目的当前值
	// 写入前像历史，推送事务整体回滚时历史随之回滚。
	if s.historyRetain > 0 {
		if err := recordHistoryPreimages(ctx, tx, vaultID, entries); err != nil {
			return nil, 0, err
		}
	}
	var latest int64
	for i := range entries {
		rev, err := nextRevision(ctx, tx, vaultID)
		if err != nil {
			return nil, 0, err
		}
		entries[i].Revision = rev
		latest = rev
		if entries[i].Deleted {
			_, err = tx.Exec(ctx,
				`INSERT INTO entries (vault_id, kind, grp, key, ciphertext, revision, deleted, updated_at)
				 VALUES ($1, $2, $3, $4, NULL, $5, TRUE, now())
				 ON CONFLICT (vault_id, kind, grp, key)
				 DO UPDATE SET ciphertext = NULL, revision = EXCLUDED.revision, deleted = TRUE, updated_at = EXCLUDED.updated_at`,
				vaultID, entries[i].Kind, entries[i].Grp, entries[i].Key, rev)
		} else {
			_, err = tx.Exec(ctx,
				`INSERT INTO entries (vault_id, kind, grp, key, ciphertext, revision, deleted, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, FALSE, now())
				 ON CONFLICT (vault_id, kind, grp, key)
				 DO UPDATE SET ciphertext = EXCLUDED.ciphertext, revision = EXCLUDED.revision, deleted = FALSE, updated_at = EXCLUDED.updated_at`,
				vaultID, entries[i].Kind, entries[i].Grp, entries[i].Key, entries[i].Ciphertext, rev)
		}
		if err != nil {
			return nil, 0, err
		}
	}
	if s.historyRetain > 0 {
		if err := pruneHistory(ctx, tx, vaultID, s.historyRetain); err != nil {
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
	if err := validateVaultName(vault); err != nil {
		return nil, 0, err
	}
	vaultID, err := lookupVault(ctx, s.pool, userID, vault)
	if err != nil {
		return nil, 0, err
	}
	var latest int64
	if err := s.pool.QueryRow(ctx, `SELECT seq FROM vaults WHERE id = $1`, vaultID).Scan(&latest); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT kind, grp, key, ciphertext, revision, deleted, updated_at FROM entries
		 WHERE vault_id = $1 AND revision > $2 ORDER BY revision`, vaultID, since)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Kind, &e.Grp, &e.Key, &e.Ciphertext, &e.Revision, &e.Deleted, &e.UpdatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	return entries, latest, rows.Err()
}
