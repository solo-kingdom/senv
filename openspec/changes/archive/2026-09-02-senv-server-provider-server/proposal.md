## Why

driver `senv-server-provider-driver` 已定方向：新增零知识密文托管服务 senv-server（Postgres 多用户、per-entry 乐观锁），作为 git 之外的第二种 provider 远端。本子 change 落地服务端。

## What Changes

- 新增独立二进制 `senv-server`：HTTP API（v1 前缀）、Postgres 持久化
- 多用户模型：用户 + vault，Bearer token 鉴权（库中只存 hash）
- 数据模型：users / vaults / vault_metadata（salt、passwordKey 等密文 blob 托管）/ entries（per-entry revision）
- 同步协议：增量 pull（since revision）+ 乐观锁 push（base_revision 校验，409 冲突）
- schema 版本管理与启动时校验

## Capabilities

### New Capabilities
- `server-api`: senv-server 的 HTTP 同步接口、数据模型与乐观锁并发语义
- `server-auth`: 用户与 token 的签发、校验、吊销行为

### Modified Capabilities
（无）

## Impact

- 新增 `senv-server` main 包与 `internal/server/`（handler、store、schema 迁移）
- 新增外部依赖：pgx/v5（Postgres 驱动）
- 部署：需要 Postgres 实例；TLS 由反代负责（文档说明）
