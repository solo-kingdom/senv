## 1. 骨架与 schema

- [x] 1.1 新建 `senv-server` main 包与 `internal/server` 结构，接入 pgx/v5
- [x] 1.2 实现 schema 迁移（users/tokens/vaults/vault_metadata/entries + schema_migrations）与启动版本校验

## 2. 鉴权

- [x] 2.1 admin 子命令：创建用户并签发 token（明文仅展示一次，库存 SHA-256 哈希）、吊销 token
- [x] 2.2 Bearer 认证中间件：401 语义、跨用户 vault 访问返回 404

## 3. 同步 API

- [x] 3.1 vault metadata 读写接口（不透明 blob）
- [x] 3.2 乐观锁批量推送（base_revision 校验、409 冲突清单、单事务、单批上限、大小上限）
- [x] 3.3 增量拉取（since、含删除标记、空增量语义）

## 4. 验证

- [x] 4.1 store/handler 单测 + 冲突与隔离集成测试（测试用临时 Postgres 或 testcontainers）
- [x] 4.2 `make check` 全绿；`openspec validate --strict --type change senv-server-provider-server` 通过
