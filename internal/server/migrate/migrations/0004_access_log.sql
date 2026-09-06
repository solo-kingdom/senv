-- 系统日志（安全审计）：每个受保护 API 请求一条事件流水。
-- 只记录连接与身份元数据，绝不含 token、口令或密文内容。
CREATE TABLE IF NOT EXISTS access_log (
    id BIGSERIAL PRIMARY KEY,
    ts TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip TEXT NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    client_id BIGINT,
    user_id BIGINT,
    outcome TEXT NOT NULL,
    reason TEXT
);

CREATE INDEX IF NOT EXISTS access_log_ts_idx ON access_log(ts);
CREATE INDEX IF NOT EXISTS access_log_user_idx ON access_log(user_id, ts);
CREATE INDEX IF NOT EXISTS access_log_client_idx ON access_log(client_id, ts);
CREATE INDEX IF NOT EXISTS access_log_outcome_idx ON access_log(outcome, ts);
