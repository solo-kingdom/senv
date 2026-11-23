-- 条目历史归因：记录「何时被替换」与「谁替换的」（token 推出的 client/user）。
-- replaced_at 是变更时刻（旧列 created_at 是前像被写入的时刻，不能当时间线用）；
-- 旧行按 created_at 回填。actor 列为空表示旧版本/历史开启前写入。
ALTER TABLE entries_history ADD COLUMN IF NOT EXISTS replaced_at TIMESTAMPTZ;
ALTER TABLE entries_history ADD COLUMN IF NOT EXISTS actor_client_id BIGINT;
ALTER TABLE entries_history ADD COLUMN IF NOT EXISTS actor_user_id BIGINT;

UPDATE entries_history SET replaced_at = created_at WHERE replaced_at IS NULL;

CREATE INDEX IF NOT EXISTS entries_history_replaced_idx
    ON entries_history(vault_id, replaced_at DESC);
