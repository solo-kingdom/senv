-- vault metadata 的前像历史：覆盖前保存上一版 blob（保留 N 版，N 复用 --history-retain）。
-- metadata（salt/passwordKey）是唯一「覆盖即不可用」的对象，必须可回滚。
CREATE TABLE IF NOT EXISTS vault_metadata_history (
    id BIGSERIAL PRIMARY KEY,
    vault_id BIGINT NOT NULL REFERENCES vaults(id),
    blob BYTEA NOT NULL,
    replaced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS vault_metadata_history_vault_idx
    ON vault_metadata_history(vault_id, replaced_at DESC, id DESC);
