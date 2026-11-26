// 条目历史：推送前像留存、按条目保留裁剪与查询。
// 历史行只含密文、revision/时间戳与操作者标识（client/user id），server 保持零知识。
package store

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// itoa 是小整数转字符串的局部简写（占位符序号生成用）
func itoa(n int) string { return strconv.Itoa(n) }

// DefaultHistoryRetain 是每条目默认保留的历史版本数
const DefaultHistoryRetain = 3

// HistoryVersion 是条目的一个历史版本（前像密文 + 归因）
type HistoryVersion struct {
	Kind       string    `json:"kind"`
	Grp        string    `json:"grp"`
	Key        string    `json:"key"`
	Ciphertext []byte    `json:"ciphertext,omitempty"`
	Revision   int64     `json:"revision"`
	CreatedAt  time.Time `json:"created_at"`
	Deleted    bool      `json:"deleted"`
	// ReplacedAt 是该前像被替换的时刻（变更时间）；旧行为前像写入时间回填。
	ReplacedAt time.Time `json:"replaced_at"`
	// Actor 是服务端从 token 推出的操作者（旧版本写入的行为空）。
	ActorClientID   int64  `json:"actor_client_id,omitempty"`
	ActorUserID     int64  `json:"actor_user_id,omitempty"`
	ActorClientName string `json:"actor_client_name,omitempty"`
}

// SetHistoryRetain 设置每条目保留的历史版本数；<=0 关闭历史留存
func (s *pgStore) SetHistoryRetain(n int) { s.historyRetain = n }

// SetHistoryPrune 控制是否在 push 事务内裁剪历史。默认 true（兼容旧行为）；
// 设为 false 后由 admin cron 执行 `senv-server admin history-prune --retain N`，
// 使 serve 角色对 entries_history 只需 INSERT+SELECT（历史不可被服务进程改写）。
func (s *pgStore) SetHistoryPrune(prune bool) { s.historyPrune = prune }

// recordHistoryPreimages 在推送应用变更前，把受影响条目的当前值批量写入历史。
// 仅当前像与新值不同（密文或删除标记变化）才写：相同密文重推、tombstone 重推
// 不消耗历史版本。单条 INSERT..SELECT JOIN unnest：条目不存在时跳过新条目。
func recordHistoryPreimages(ctx context.Context, tx pgx.Tx, vaultID int64, entries []Entry, userID, clientID int64) error {
	if len(entries) == 0 {
		return nil
	}
	kinds := make([]string, len(entries))
	grps := make([]string, len(entries))
	keys := make([]string, len(entries))
	cts := make([][]byte, len(entries))
	dels := make([]bool, len(entries))
	for i, e := range entries {
		kinds[i] = e.Kind
		grps[i] = e.Grp
		keys[i] = e.Key
		if !e.Deleted {
			cts[i] = e.Ciphertext
		}
		dels[i] = e.Deleted
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO entries_history
		   (vault_id, kind, grp, key, ciphertext, revision, created_at, deleted,
		    replaced_at, actor_client_id, actor_user_id)
		 SELECT e.vault_id, e.kind, e.grp, e.key, e.ciphertext, e.revision, e.updated_at, e.deleted,
		        now(), $2, $3
		 FROM unnest($4::text[], $5::text[], $6::text[], $7::bytea[], $8::boolean[])
		      AS u(kind, grp, key, new_ct, new_del)
		 JOIN entries e
		   ON e.vault_id = $1 AND e.kind = u.kind AND e.grp = u.grp AND e.key = u.key
		 WHERE (e.ciphertext IS DISTINCT FROM u.new_ct OR e.deleted IS DISTINCT FROM u.new_del)`,
		vaultID, clientID, userID, kinds, grps, keys, cts, dels)
	return err
}

// pruneHistory 把本次推送涉及条目的历史裁剪到最近 retain 版（仅扫描受影响的 (kind, grp, key) 子集）。
func pruneHistory(ctx context.Context, tx pgx.Tx, vaultID int64, retain int, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	kinds := make([]string, len(entries))
	grps := make([]string, len(entries))
	keys := make([]string, len(entries))
	for i, e := range entries {
		kinds[i] = e.Kind
		grps[i] = e.Grp
		keys[i] = e.Key
	}
	_, err := tx.Exec(ctx,
		`WITH target_keys AS (
		   SELECT DISTINCT kind, grp, key
		   FROM unnest($3::text[], $4::text[], $5::text[]) AS u(kind, grp, key)
		 )
		 DELETE FROM entries_history
		 WHERE vault_id = $1 AND id IN (
		   SELECT id FROM (
		     SELECT h.id, ROW_NUMBER() OVER (
		       PARTITION BY h.kind, h.grp, h.key ORDER BY h.revision DESC, h.id DESC) AS rn
		     FROM entries_history h
		     JOIN target_keys tk ON h.kind = tk.kind AND h.grp = tk.grp AND h.key = tk.key
		     WHERE h.vault_id = $1
		   ) ranked WHERE ranked.rn > $2
		 )`, vaultID, retain, kinds, grps, keys)
	return err
}

// PruneAllHistory 由 admin 侧清理：把所有 vault 的条目历史与 metadata 历史
// 裁剪到最近 retain 版。供 `--history-prune-mode admin` 部署配合 cron 使用；
// 返回总删除行数。
func (s *pgStore) PruneAllHistory(ctx context.Context, retain int) (int64, error) {
	if retain <= 0 {
		return 0, validationErrorf("history retain 必须大于 0")
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM entries_history WHERE id IN (
		   SELECT id FROM (
		     SELECT id, ROW_NUMBER() OVER (
		       PARTITION BY vault_id, kind, grp, key ORDER BY revision DESC, id DESC) AS rn
		     FROM entries_history
		   ) ranked WHERE ranked.rn > $1
		 )`, retain)
	if err != nil {
		return 0, err
	}
	metaTag, err := s.pool.Exec(ctx,
		`DELETE FROM vault_metadata_history WHERE id IN (
		   SELECT id FROM (
		     SELECT id, ROW_NUMBER() OVER (
		       PARTITION BY vault_id ORDER BY replaced_at DESC, id DESC) AS rn
		     FROM vault_metadata_history
		   ) ranked WHERE ranked.rn > $1
		 )`, retain)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected() + metaTag.RowsAffected(), nil
}

// HistoryFilter 过滤历史查询；零值字段表示不过滤
type HistoryFilter struct {
	Kind  string
	Grp   string
	Key   string
	Limit int
}

// ListHistory 查询条目历史。指定 key（或 grp+kind）时按该条目 revision 新到旧
// 返回；否则返回 vault 级按变更时间（replaced_at）新到旧的最近变更。只能查自己的 vault。
func (s *pgStore) ListHistory(ctx context.Context, userID int64, vault string, f HistoryFilter) ([]HistoryVersion, error) {
	if err := validateVaultName(vault); err != nil {
		return nil, err
	}
	vaultID, err := lookupVault(ctx, s.pool, userID, vault)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}

	where := `WHERE h.vault_id = $1`
	args := []any{vaultID}
	next := 2
	addFilter := func(column, value string) {
		args = append(args, value)
		where += ` AND h.` + column + ` = $` + itoa(next)
		next++
	}
	if f.Kind != "" {
		addFilter("kind", f.Kind)
	}
	if f.Grp != "" {
		addFilter("grp", f.Grp)
	}
	if f.Key != "" {
		addFilter("key", f.Key)
	}
	order := `ORDER BY COALESCE(h.replaced_at, h.created_at) DESC, h.id DESC`
	if f.Key != "" {
		order = `ORDER BY h.revision DESC, h.id DESC`
	}
	args = append(args, f.Limit)

	rows, err := s.pool.Query(ctx,
		`SELECT h.kind, h.grp, h.key, h.ciphertext, h.revision, h.created_at, h.deleted,
		        COALESCE(h.replaced_at, h.created_at),
		        COALESCE(h.actor_client_id, 0), COALESCE(h.actor_user_id, 0),
		        COALESCE(c.name, '')
		 FROM entries_history h
		 LEFT JOIN clients c ON c.id = h.actor_client_id `+where+` `+order+` LIMIT $`+itoa(next), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := []HistoryVersion{}
	for rows.Next() {
		var h HistoryVersion
		if err := rows.Scan(&h.Kind, &h.Grp, &h.Key, &h.Ciphertext, &h.Revision, &h.CreatedAt,
			&h.Deleted, &h.ReplacedAt, &h.ActorClientID, &h.ActorUserID, &h.ActorClientName); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}
