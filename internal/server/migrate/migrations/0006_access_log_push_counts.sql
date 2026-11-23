-- 访问日志补充推送规模：单次 push 的条目数与删除数，供「谁在何时删了多少」取证。
-- 非 push 请求保持 0。
ALTER TABLE access_log ADD COLUMN IF NOT EXISTS n_entries INT NOT NULL DEFAULT 0;
ALTER TABLE access_log ADD COLUMN IF NOT EXISTS n_deleted INT NOT NULL DEFAULT 0;
