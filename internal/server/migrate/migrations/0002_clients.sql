-- client 设备身份：一台注册设备一份记录，凭证挂到 client 下（存量 user 级
-- token 的 client_id 为 NULL，继续有效，仅可通过吊销停用）。
CREATE TABLE IF NOT EXISTS clients (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ,
    UNIQUE(user_id, name)
);

-- 一次性注册码：明文只在 admin 签发时展示一次，库中仅存哈希
CREATE TABLE IF NOT EXISTS registration_codes (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    code_hash BYTEA UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE tokens ADD COLUMN IF NOT EXISTS client_id BIGINT REFERENCES clients(id);
CREATE INDEX IF NOT EXISTS tokens_client_id_idx ON tokens(client_id);
CREATE INDEX IF NOT EXISTS registration_codes_user_idx ON registration_codes(user_id);
