## Context

见 proposal.md 与 driver design.md（D2/D3/D4/D5）。本文件细化服务端实现决策。

## Goals / Non-Goals

**Goals:** 可独立部署的 senv-server；schema 迁移内置；协议可被 client 子 change 直接联调。

**Non-Goals:** 不做 TLS 终结（反代负责）；不做 rate limit / 审计日志（后续迭代）；不做 vault 间共享。

## Decisions

### 结构

`senv-server` main 包（与 `senv` 平级）+ `internal/server/`（`store` 负责 SQL、`handler` 负责 HTTP、`migrate` 负责 schema）。路由用标准库 `http.ServeMux` 模式路由；DB 用 pgx/v5 连接池。管理入口（建用户/吊销 token）作为 `senv-server admin` 子命令直连数据库，不暴露 HTTP 管理面。

### schema

```sql
users(id BIGSERIAL PK, name TEXT UNIQUE NOT NULL, created_at TIMESTAMPTZ NOT NULL)
tokens(id BIGSERIAL PK, user_id BIGINT REFERENCES users, token_hash BYTEA UNIQUE NOT NULL,
       revoked_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL)
vaults(id BIGSERIAL PK, user_id BIGINT REFERENCES users, name TEXT NOT NULL,
       seq BIGINT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL,
       UNIQUE(user_id, name))
vault_metadata(vault_id BIGINT PK REFERENCES vaults, blob BYTEA NOT NULL, updated_at TIMESTAMPTZ NOT NULL)
entries(vault_id BIGINT NOT NULL REFERENCES vaults, kind TEXT NOT NULL, grp TEXT NOT NULL,
        key TEXT NOT NULL, ciphertext BYTEA, revision BIGINT NOT NULL,
        deleted BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL,
        PRIMARY KEY(vault_id, kind, grp, key))
CREATE INDEX ON entries(vault_id, revision);
```

revision 取自 `vaults.seq`（`UPDATE vaults SET seq = seq + 1 RETURNING seq`，行锁保证单调）。推送整批在一个事务内完成，天然满足「不部分写入」。

### token 哈希

token = 32 字节随机 base64url；库存 SHA-256(token)。token 本身高熵，无需慢哈希。

## Risks / Trade-offs

- `vaults.seq` 行锁在高并发写下成为热点 → 单 vault 写频率低（个人同步场景），可接受
- 整批事务大推送可能长事务 → 限制单批条目数（如 1000），超出返回明确错误
- pgx 引入新依赖树 → 仅 server 二进制使用，CLI 不受影响

## Migration Plan

`internal/server/migrate` 内置顺序迁移文件 + `schema_migrations` 表；启动时校验版本，落后则提示 `senv-server migrate`。
