package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// MetadataConflictError 表示 metadata 写入的 base_hash 与远端当前值不符：
// 期间有其它设备改过 metadata，必须由客户端重新读取后再决定（不静默覆盖）。
type MetadataConflictError struct{}

func (e *MetadataConflictError) Error() string {
	return "vault metadata 已被其他设备更新（base_hash 不匹配），请先同步"
}

// MetadataVersion 是 vault metadata 的一个历史版本（明文 blob，本身即不透明内容）
type MetadataVersion struct {
	Blob       []byte    `json:"blob"`
	ReplacedAt time.Time `json:"replaced_at"`
}

// metadataHash 返回 blob 的 sha256 十六进制串（与客户端 hashBytes 口径一致）。
func metadataHash(blob []byte) string {
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])
}

// GetMetadataHistory 返回 vault metadata 的历史版本（新到旧，最多 limit 条）。
func (s *pgStore) GetMetadataHistory(ctx context.Context, userID int64, vault string, limit int) ([]MetadataVersion, error) {
	if err := validateVaultName(vault); err != nil {
		return nil, err
	}
	vaultID, err := lookupVault(ctx, s.pool, userID, vault)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		`SELECT blob, replaced_at FROM vault_metadata_history
		 WHERE vault_id = $1 ORDER BY replaced_at DESC, id DESC LIMIT $2`, vaultID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetadataVersion{}
	for rows.Next() {
		var v MetadataVersion
		if err := rows.Scan(&v.Blob, &v.ReplacedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// recordMetadataPreimage 在覆盖前把当前 blob 写入历史，并按 historyRetain 裁剪。
// 同一事务内调用；内容未变化时调用方不写前像。
func recordMetadataPreimage(ctx context.Context, tx pgx.Tx, vaultID int64, retain int) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO vault_metadata_history (vault_id, blob, replaced_at)
		 SELECT vault_id, blob, now() FROM vault_metadata WHERE vault_id = $1`, vaultID); err != nil {
		return err
	}
	if retain > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM vault_metadata_history WHERE id IN (
			   SELECT id FROM (
			     SELECT id, ROW_NUMBER() OVER (PARTITION BY vault_id ORDER BY replaced_at DESC, id DESC) AS rn
			     FROM vault_metadata_history WHERE vault_id = $1
			   ) ranked WHERE ranked.rn > $2
			 )`, vaultID, retain); err != nil {
			return err
		}
	}
	return nil
}

// errNoMetadataRow 是内部哨兵：vault_metadata 尚无行
var errNoMetadataRow = errors.New("metadata row absent")
