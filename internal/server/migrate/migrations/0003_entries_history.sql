-- 条目历史：推送修改/删除前的前像（pre-image）密文留存。
-- 每个条目仅保留最近 N 版（N 由 server 启动参数 --history-retain 控制，默认 3）。
CREATE TABLE IF NOT EXISTS entries_history (
    id BIGSERIAL PRIMARY KEY,
    vault_id BIGINT NOT NULL REFERENCES vaults(id),
    kind TEXT NOT NULL,
    grp TEXT NOT NULL,
    key TEXT NOT NULL,
    ciphertext BYTEA,
    revision BIGINT NOT NULL,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE(vault_id, kind, grp, key, revision)
);

CREATE INDEX IF NOT EXISTS entries_history_entry_idx
    ON entries_history(vault_id, kind, grp, key, revision DESC);
CREATE INDEX IF NOT EXISTS entries_history_recent_idx
    ON entries_history(vault_id, created_at DESC);
