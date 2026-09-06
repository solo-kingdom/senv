## Context
server 现有 users/tokens 表（0001 迁移），认证中间件按 token 哈希换 user_id（internal/server/store/store.go:157-168），admin CLI 仅 create-user / revoke-token（senv-server/main.go:155-198）；client 侧 token 存 settings.json（internal/storage/types.go:32-41），HTTP 错误映射在 internal/provider/server_client.go:98-125。决策依据：driver 的 grill.md（D1–D3、D7）。

## Goals / Non-Goals
**Goals:** 设备级身份、注册闭环、屏蔽可逆、client 感知清理
**Non-Goals:** 见 proposal；不改 vault 隔离与零知识模型；不改既有 401 语义

## Decisions
1. **新表 clients / registration_codes（0002 迁移）**：clients(id, user_id, name, status, created_at, last_seen_at, UNIQUE(user_id,name))；registration_codes(id, user_id, code_hash, expires_at, used_at)。注册码哈希沿用 token 的 SHA-256 方案。备选「审批队列」被 grill 否决（需轮询，UX 重）。
2. **token 挂 client**：tokens 加可空 client_id。存量 token client_id 为 NULL 继续有效（向后兼容，避免破坏现有部署）；注册新发的 token 绑定 client；屏蔽仅作用于 client 绑定 token。备选「强制迁移存量 token」破坏性大，弃。
3. **屏蔽语义 403 + `client_blocked`**：认证链解析 token→client；client.status=blocked 时返回 403 `{"error":"client_blocked"}`。无效/缺失/吊销仍 401（维持 server-auth 既有语义）。备选「屏蔽并入 401」无法让 client 区分两种失败，弃（grill D3）。
4. **注册端点 `POST /v1/register`**：免认证，复用现有限速器；校验注册码哈希/过期/一次性（used_at 原子置位）→ 创建 client + token → 一次性返回明文 token。
5. **client 感知**：server_client.go 将 403+`client_blocked` 映射为独立的 BlockedError；命令层捕获后经 session.Manager 清解锁缓存、写审计事件、提示并按非零码退出。

## 数据流
```
注册: admin create-registration ──code──▶ senv server register ──▶ POST /v1/register ──▶ clients+tokens ──▶ settings.json
访问: client ──Bearer──▶ 中间件(限速 → token哈希 → client状态) ──▶ handler
屏蔽: admin block-client ──▶ clients.status=blocked ──▶ 下一次请求 403 client_blocked ──▶ client 清解锁缓存
```

## 错误处理策略
- 注册码无效/过期/已用：统一 400，不区分具体原因（防枚举），计入限速
- 设备名冲突：409，提示更换设备名
- client 收到 403 但响应不是 `client_blocked`（旧 server 场景防御）：按普通错误处理，**不**清解锁缓存，避免误清
- 清理解锁缓存失败：提示用户手动 `senv session clear`，命令仍以非零码退出

## CLI 使用示例
```
senv-server admin create-registration --user alice --expires 24h
# → 一次性注册码（如 R7XK-...），过期后作废
senv server register --address https://senv.example.com --code R7XK-... --name my-laptop
# → token 写入 settings.json（0600 原子写），明文仅此一次展示
senv-server admin list-clients --user alice
senv-server admin block-client --client my-laptop --user alice
senv-server admin unblock-client --client my-laptop --user alice
```

## Risks / Trade-offs
- [403 语义向持有效凭证者暴露「存在且被屏蔽」] → 该凭证本已通过认证，风险可接受；未知 token 仍 401
- [存量 token 无设备归属、不可屏蔽] → 可用既有 revoke-token 吊销；文档引导迁移到注册式 client
- [屏蔽后 client 仍有本地密文] → 零知识下密文对无口令者无意义，且解锁缓存已清；grill D2 已接受

## Migration Plan
0002 仅加表加列，向后兼容；发布顺序 server 先行、client 后行（旧 client 无 403 感知，行为不变）。回滚：迁移不删数据，回退二进制即可。

## Open Questions
无
