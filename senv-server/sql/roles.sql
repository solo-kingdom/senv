-- senv-server 数据库角色模板（server-hardening-audit-db-roles 单一事实源，deploy-docs 引用本文件）
--
-- 前提：以超级用户或库属主执行；角色口令由部署方管理，本文件不含任何口令；
--       schema 迁移（senv-server migrate）仍需高权限执行，本模板不负责建表。
--
-- 用法：
--   CREATE ROLE senv_server LOGIN PASSWORD '...';   -- serve 运行时（--dsn / SENV_SERVER_DSN）
--   CREATE ROLE senv_admin  LOGIN PASSWORD '...';   -- admin CLI 与 logs-prune 定时任务
--   \i roles.sql
--
-- 把下方 senv 替换为实际库名。

BEGIN;

-- serve 运行时角色：业务表正常读写
GRANT CONNECT ON DATABASE senv TO senv_server;
GRANT USAGE ON SCHEMA public TO senv_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON
    users, tokens, vaults, vault_metadata, entries, entries_history,
    clients, registration_codes
TO senv_server;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO senv_server;

-- 访问日志：仅可追加（INSERT）与查询（SELECT），不可 UPDATE/DELETE——
-- 拿到 serve DSN 无法抹除访问痕迹。
-- 后果：serve 的 --logs-retain-days 自动清理在此角色下会权限失败
--（best-effort 记错误日志，不影响服务）。生产部署应设 --logs-retain-days 0，
-- 由 senv_admin 角色的 cron 执行 `senv-server admin logs-prune --before <date>`。
GRANT SELECT, INSERT ON access_log TO senv_server;

-- admin 角色：日志清理与日常管理（含 UPDATE/DELETE access_log）
GRANT CONNECT ON DATABASE senv TO senv_admin;
GRANT USAGE ON SCHEMA public TO senv_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO senv_admin;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO senv_admin;

COMMIT;
