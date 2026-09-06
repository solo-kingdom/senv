// client 设备身份与一次性注册码的存储层。
// 屏蔽（block）是绑定在 client 实体上的可逆状态，区别于绑定单份凭证的
// 不可逆吊销（revoke）；被屏蔽 client 的 server 数据不受影响（数据归属 user）。
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// client 状态取值
const (
	ClientStatusActive  = "active"
	ClientStatusBlocked = "blocked"
)

// ErrNameConflict 表示同用户下已存在同名 client（HTTP 层映射为 409）
var ErrNameConflict = errors.New("client name conflict")

// Client 是一台注册设备
type Client struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// AuthResult 是一次认证解析的结果。ClientID 为 0 表示无 client 归属的
// 存量 user 级 token（0002 迁移前的存量数据，向后兼容保留）。
type AuthResult struct {
	UserID        int64
	ClientID      int64
	ClientStatus  string
	ClientBlocked bool
}

// GenerateRegistrationCode 生成一次性注册码并入库（仅存哈希），返回明文。
// 明文只在签发时展示一次；注册成功或过期后注册码即失效。
func (s *Store) CreateRegistrationCode(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", validationErrorf("注册码有效期必须为正")
	}
	code, err := GenerateToken()
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO registration_codes (user_id, code_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hashToken(code), time.Now().Add(ttl))
	if err != nil {
		return "", fmt.Errorf("签发注册码失败: %w", err)
	}
	return code, nil
}

// RegisterClient 用一次性注册码注册新 client 并签发其专属 token，返回一次性
// 明文 token。整个流程单事务：注册码校验（存在/未用/未过期）、设备名查重、
// client 创建、token 签发原子完成；任何一步失败注册码不被消费。
// 无效/过期/已用的注册码统一返回 ErrNotFound（HTTP 层映射为 400 通用消息，
// 不区分具体原因以防枚举）。
func (s *Store) RegisterClient(ctx context.Context, code, name string) (string, *Client, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback(ctx)

	var userID int64
	err = tx.QueryRow(ctx,
		`SELECT user_id FROM registration_codes
		 WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()`,
		hashToken(code)).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, ErrNotFound
		}
		return "", nil, err
	}

	// 同用户下设备名唯一；冲突时不消费注册码，提示改名后重试
	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM clients WHERE user_id = $1 AND name = $2)`,
		userID, name).Scan(&exists)
	if err != nil {
		return "", nil, err
	}
	if exists {
		return "", nil, ErrNameConflict
	}

	if _, err := tx.Exec(ctx,
		`UPDATE registration_codes SET used_at = now() WHERE code_hash = $1`, hashToken(code)); err != nil {
		return "", nil, err
	}

	token, err := GenerateToken()
	if err != nil {
		return "", nil, err
	}
	var client Client
	err = tx.QueryRow(ctx,
		`INSERT INTO clients (user_id, name, status) VALUES ($1, $2, $3)
		 RETURNING id, user_id, name, status, created_at, last_seen_at`,
		userID, name, ClientStatusActive).Scan(
		&client.ID, &client.UserID, &client.Name, &client.Status, &client.CreatedAt, &client.LastSeenAt)
	if err != nil {
		return "", nil, fmt.Errorf("创建 client 失败: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO tokens (user_id, client_id, token_hash) VALUES ($1, $2, $3)`,
		userID, client.ID, hashToken(token)); err != nil {
		return "", nil, fmt.Errorf("签发 token 失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, err
	}
	return token, &client, nil
}

// SetClientStatus 屏蔽/解封指定 client。userID<0 表示不限用户（admin 全局操作）。
func (s *Store) SetClientStatus(ctx context.Context, userID int64, name, status string) error {
	if status != ClientStatusActive && status != ClientStatusBlocked {
		return validationErrorf("未知的 client 状态 %q", status)
	}
	query := `UPDATE clients SET status = $1 WHERE name = $2`
	args := []any{status, name}
	if userID >= 0 {
		query += ` AND user_id = $3`
		args = append(args, userID)
	}
	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListClients 列出 client；userID<0 表示全部用户。按创建时间正序。
func (s *Store) ListClients(ctx context.Context, userID int64) ([]Client, error) {
	query := `SELECT id, user_id, name, status, created_at, last_seen_at FROM clients`
	args := []any{}
	if userID >= 0 {
		query += ` WHERE user_id = $1`
		args = append(args, userID)
	}
	query += ` ORDER BY created_at, id`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clients := []Client{}
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.Status, &c.CreatedAt, &c.LastSeenAt); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

// UserIDByName 按用户名查 id（admin 命令使用）；不存在返回 ErrNotFound
func (s *Store) UserIDByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM users WHERE name = $1`, name).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return id, nil
}

// AuthenticateWithClient 用 token 换取认证结果（user + 所属 client 状态）。
// 无效或已吊销返回 ErrNotFound（与 Authenticate 一致，不泄露存在性）。
// 屏蔽状态不在此判定——client 被屏蔽时 token 仍能解析，由 HTTP 层返回 403。
func (s *Store) AuthenticateWithClient(ctx context.Context, token string) (AuthResult, error) {
	var res AuthResult
	var clientID pgxNullInt64
	var status pgxNullString
	err := s.pool.QueryRow(ctx,
		`SELECT t.user_id, c.id, c.status
		 FROM tokens t
		 LEFT JOIN clients c ON c.id = t.client_id
		 WHERE t.token_hash = $1 AND t.revoked_at IS NULL`,
		hashToken(token)).Scan(&res.UserID, &clientID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AuthResult{}, ErrNotFound
		}
		return AuthResult{}, err
	}
	res.ClientID = clientID.value
	res.ClientStatus = status.value
	res.ClientBlocked = res.ClientStatus == ClientStatusBlocked
	return res, nil
}

// TouchClient 节流地刷新 client 最近活跃时间（成功认证后调用，best-effort）。
// 仅当上次刷新超过一分钟前才写，避免每个请求一次磁盘写放大。
func (s *Store) TouchClient(ctx context.Context, clientID int64) {
	if clientID <= 0 {
		return
	}
	_, _ = s.pool.Exec(ctx,
		`UPDATE clients SET last_seen_at = now()
		 WHERE id = $1 AND (last_seen_at IS NULL OR last_seen_at < now() - interval '1 minute')`,
		clientID)
}

// pgxNullInt64/pgxNullString 是 LEFT JOIN 可空列的轻量扫描目标
type pgxNullInt64 struct{ value int64 }

func (n *pgxNullInt64) Scan(src any) error {
	if src == nil {
		n.value = 0
		return nil
	}
	v, ok := src.(int64)
	if !ok {
		return fmt.Errorf("cannot scan %T into int64", src)
	}
	n.value = v
	return nil
}

type pgxNullString struct{ value string }

func (n *pgxNullString) Scan(src any) error {
	if src == nil {
		n.value = ""
		return nil
	}
	v, ok := src.(string)
	if !ok {
		return fmt.Errorf("cannot scan %T into string", src)
	}
	n.value = v
	return nil
}
