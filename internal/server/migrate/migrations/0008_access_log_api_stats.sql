-- 扩展 access_log 表以支持接口访问统计与 API Key (client/token) 专项统计：
-- 增量补齐 route_template、status_code、duration_ms、auth_type 与必要索引，
-- 保持与现有 outcome/client_id/user_id/n_entries/n_deleted 完全兼容。
ALTER TABLE access_log ADD COLUMN IF NOT EXISTS route_template TEXT NOT NULL DEFAULT '';
ALTER TABLE access_log ADD COLUMN IF NOT EXISTS status_code INT NOT NULL DEFAULT 0;
ALTER TABLE access_log ADD COLUMN IF NOT EXISTS duration_ms INT NOT NULL DEFAULT 0;
ALTER TABLE access_log ADD COLUMN IF NOT EXISTS auth_type TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS access_log_ts_desc_idx ON access_log(ts DESC);
CREATE INDEX IF NOT EXISTS access_log_route_ts_idx ON access_log(route_template, ts DESC);
CREATE INDEX IF NOT EXISTS access_log_status_ts_idx ON access_log(status_code, ts DESC);
