// 条目历史：推送前像留存、按条目保留裁剪与查询。
// 历史行只含密文与 revision/时间戳，server 保持零知识。
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

// HistoryVersion 是条目的一个历史版本（前像密文）
type HistoryVersion struct {
	Kind       string    `json:"kind"`
	Grp        string    `json:"grp"`
	Key        string    `json:"key"`
	Ciphertext []byte    `json:"ciphertext,omitempty"`
	Revision   int64     `json:"revision"`
	CreatedAt  time.Time `json:"created_at"`
	Deleted    bool      `json:"deleted"`
}

// SetHistoryRetain 设置每条目保留的历史版本数；<=0 关闭历史留存
func (s *Store) SetHistoryRetain(n int) { s.historyRetain = n }

// recordHistoryPreimages 在推送应用变更前，把受影响条目的当前值写入历史。
// 单条 INSERT..SELECT：条目不存在时插入 0 行，天然跳过新条目。
func recordHistoryPreimages(ctx context.Context, tx pgx.Tx, vaultID int64, entries []Entry) error {
	for _, e := range entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO entries_history (vault_id, kind, grp, key, ciphertext, revision, created_at, deleted)
			 SELECT vault_id, kind, grp, key, ciphertext, revision, updated_at, deleted
			 FROM entries
			 WHERE vault_id = $1 AND kind = $2 AND grp = $3 AND key = $4`,
			vaultID, e.Kind, e.Grp, e.Key); err != nil {
			return err
		}
	}
	return nil
}

// pruneHistory 把 vault 内每个条目的历史裁剪到最近 retain 版。
// 每次推送执行一次，整 vault 分区排名，vault 规模有界（条目数 × N）。
func pruneHistory(ctx context.Context, tx pgx.Tx, vaultID int64, retain int) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM entries_history
		 WHERE vault_id = $1 AND id IN (
		   SELECT id FROM (
		     SELECT id, ROW_NUMBER() OVER (
		       PARTITION BY kind, grp, key ORDER BY revision DESC, id DESC) AS rn
		     FROM entries_history WHERE vault_id = $1
		   ) ranked WHERE ranked.rn > $2
		 )`, vaultID, retain)
	return err
}

// HistoryFilter 过滤历史查询；零值字段表示不过滤
type HistoryFilter struct {
	Kind  string
	Grp   string
	Key   string
	Limit int
}

// ListHistory 查询条目历史。指定 key（或 grp+kind）时按该条目 revision 新到旧
// 返回；否则返回 vault 级按写入时间新到旧的最近变更。只能查自己的 vault。
func (s *Store) ListHistory(ctx context.Context, userID int64, vault string, f HistoryFilter) ([]HistoryVersion, error) {
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

	where := `WHERE vault_id = $1`
	args := []any{vaultID}
	next := 2
	addFilter := func(column, value string) {
		args = append(args, value)
		where += ` AND ` + column + ` = $` + itoa(next)
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
	order := `ORDER BY created_at DESC, id DESC`
	if f.Key != "" {
		order = `ORDER BY revision DESC, id DESC`
	}
	args = append(args, f.Limit)

	rows, err := s.pool.Query(ctx,
		`SELECT kind, grp, key, ciphertext, revision, created_at, deleted
		 FROM entries_history `+where+` `+order+` LIMIT $`+itoa(next), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := []HistoryVersion{}
	for rows.Next() {
		var h HistoryVersion
		if err := rows.Scan(&h.Kind, &h.Grp, &h.Key, &h.Ciphertext, &h.Revision, &h.CreatedAt, &h.Deleted); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}
