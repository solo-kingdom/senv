## Context

现状与动机见 proposal.md 及验证记录。核心约束：

- 端到端加密：所有持久化内容在客户端已是 AES-256-GCM 密文，server 永远只见密文（零知识托管）
- 已收敛的用户决策：server 与 git **并存**；Postgres 多用户；**per-entry 乐观锁**；**本地缓存离线兜底**（复用现有文件格式）；CLI 双向 import/export；metadata（salt/passwordKey）**密文托管**（方案 1）
- 现有 `internal/storage.Manager` 约 50 个方法，`cmd/*` 有 7+ 处直接构造调用

## Goals / Non-Goals

**Goals:**

- CLI 侧引入围绕 push/pull 语义的窄 Provider 抽象，git 与 server 作为两种 remote 并存
- senv-server：独立二进制，HTTP API + Postgres，多用户 token 鉴权，per-entry revision 乐观锁
- 离线可用：本地缓存即工作副本，断网可读写，恢复后同步
- git ↔ server 双向迁移工具

**Non-Goals:**

- server 不做任何明文处理（无法做搜索、转换、web 查看器）
- v1 不做冲突自动合并，只做检测 + 人工解决
- 不改变现有加密格式与 PBKDF2 参数
- 不做 web UI、不做多 vault 共享/协作（多用户指多个独立账号，非协作编辑）

## Decisions

### D1: Provider 抽象围绕 push/pull，不抽象 storage 全部方法

server 与 git 对称：两者都是「本地文件存储 + remote 同步通道」。本地缓存直接复用现有 `storage.Manager` 文件格式，读写路径不变；Provider 接口只负责同步（Push/Pull/Status）。cmd 层新增统一构造入口，按配置选择 provider。

- 替代方案：抽象全部 50 个 storage 方法 → 接口臃肿，server client 要逐个代理，离线兜底还得再叠一层缓存，否掉

### D2: senv-server 为独立二进制，Go 标准库 net/http + pgx

server 与 CLI 同仓（`cmd/` 平级或 `senv-server` main 包），独立部署单元。路由用标准库 `http.ServeMux`（Go 1.22+ 模式路由够用），Postgres 驱动用 pgx/v5（事实标准，支持连接池）。不引入 web 框架，符合项目「避免过度设计」规范。

- 替代方案：gRPC → 引入 protobuf 工具链，对纯 CRUD+同步收益低；chi/gin → 标准库已够用

### D3: 数据模型：users / vaults / vault_metadata / entries

```
users(id, name, token_hash, created_at)
vaults(id, user_id, name, created_at)
vault_metadata(vault_id, salt, password_key, settings_blob, updated_at)  -- 全密文/非敏感
entries(vault_id, kind, grp, key, ciphertext, revision, updated_at, deleted,
         PRIMARY KEY(vault_id, kind, grp, key))
  -- kind ∈ {env, env_meta, text, config, config_index}
  -- revision: per-entry 单调递增整数，乐观锁版本号
```

- per-entry revision（而非全局）：写冲突面最小，pull 增量按 `revision > since` 过滤需要 vault 级单调序列 → 用 vault 级 sequence（`vaults.seq` 或独立序列表），entry 的 revision 取自该序列，同时满足「增量拉取有序」与「写冲突按 entry 判定」
- 软删除（deleted 标记）：pull 增量能感知删除，物理清理由后台策略负责（v1 手动）

### D4: 同步协议：增量 pull + 乐观锁 push

- **Pull**: `GET /v1/vaults/{id}/entries?since={revision}` → 增量条目流 + 最新 revision
- **Push**: `POST /v1/vaults/{id}/entries` 批量提交，每条带 `base_revision`；server 校验 `base_revision == 当前 revision`，不符返回 409 + 服务端当前值，客户端重新 pull 后由用户决定覆盖或保留
- 本地缓存维护 `last_synced_revision`，与 git 的 local/remote 关系同构

### D5: 鉴权双层分离，metadata 密文托管（方案 1）

- server 账号：Bearer token（用户创建时生成，可吊销；存储只存 hash）
- vault 口令：永不离开客户端；salt/passwordKey 作为密文 blob 存于 `vault_metadata`，新机器凭 server 账号拉取后本地派生 key
- 传输层必须 TLS（部署文档责任，server 本身可跑明文 HTTP 便于反代）

### D6: 离线兜底 = 本地缓存即工作副本

server provider 模式下，所有读写落在本地缓存目录（复用 `storage.Manager` 格式），同步显式触发（`senv sync`）或写后自动尝试。断网时写操作照常成功，标记 dirty，恢复后 push。

### D7: 迁移走 CLI 双向子命令

`senv migrate to-server` / `senv migrate from-server`：逐条读取源 provider 的密文条目写入目标。server 不碰明文，做不了转换，所以只能在 CLI 侧做。

### 子 change 拆分

| 子 change | 范围 | 依赖 |
|-----------|------|------|
| `senv-server-provider-interface` | Provider 窄接口 + cmd 统一构造入口 + 现有 git 流程接入新抽象（行为不变） | 无 |
| `senv-server-provider-server` | senv-server 二进制：schema、API、token 鉴权、乐观锁 | interface（契约） |
| `senv-server-provider-client` | CLI server provider：HTTP client、本地缓存、同步引擎、离线兜底 | interface + server（联调） |
| `senv-server-provider-migrate` | git ↔ server 双向迁移 CLI | client |

## Risks / Trade-offs

- server 单点可用性 → 本地缓存兜底，断网可读写（D6）
- DB 泄漏后可离线爆破 vault 口令 → PBKDF2 100k 迭代 + 部署文档要求强口令；token 只存 hash 降低拖库直接损失
- token 泄漏 → 可吊销、建议 TLS；v1 不做 token 过期轮转
- 并发写冲突靠用户人工解决 → 错误信息必须给出冲突 entry 清单与解决指引
- server 与 CLI 版本漂移 → API 路径带 `v1` 前缀，server 启动校验 schema 版本

## Migration Plan

1. interface 子 change 为纯重构，不改变现有 git 模式行为，先行落地
2. server/client 落地后，存量 git 用户不受影响；自愿通过 migrate 子命令迁移
3. 回滚：server 数据可随时 `migrate from-server` 导回 git 仓，无锁定
