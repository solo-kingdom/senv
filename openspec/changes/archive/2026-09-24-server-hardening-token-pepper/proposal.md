## Why

token 当前存裸 SHA-256：数据库整库泄露时，若 token 熵不足或出现实现退化，可离线反查/批量比对。改为 HMAC（server 侧 pepper，不进 DB）后，DB 泄露单独不足以验证 token。编排见 `server-hardening-driver`；决策依据见其 design.md 决策 3。

实现对照：`internal/server/store/store.go` / `clients.go`（token 哈希生成与 `AuthenticateWithClient` 比对路径）、`senv-server/main.go`（env 解析）。注意认证缓存以 token 哈希派生键为键（server-auth spec「认证结果缓存」），pepper 只改哈希函数，键派生逻辑不受影响。

## What Changes

- token 哈希函数改为 `HMAC-SHA256(key=pepper, token)`；pepper 来自 env `SENV_SERVER_TOKEN_PEPPER`（可空：空 = 旧 SHA-256 行为，默认不变，便于灰度）
- 认证比对路径：先按当前配置哈希查询；启用 pepper 后若未命中，回退一次无 pepper 的 SHA-256 比对（兼容存量 token），命中记 slog 慢日志提示「该 token 仍为旧哈希，请轮换」
- `admin create-user` / `create-registration` 签发的 token 一律用当前配置哈希存储
- 轮换指引（revoke + 重新 create-registration / create-user）写入文档，由 deploy-docs 切片落位；本切片在文档有交叉处留引用锚点

**安全性分析**：pepper 仅经 env 注入、不落库不入日志；认证失败语义不变（401 不泄露存在性，回退比对也无时序/响应差异）。

## Non-goals

- 强制 pepper 非空（默认行为不变）
- 自动批量重哈希（需明文 token，违背「明文只展示一次」）
- 其余三个加固项

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | store 哈希/认证路径 + main env |

## 验收标准

- [ ] 配置 pepper 后新签发 token 为 HMAC 哈希且认证通过；无 pepper 环境行为与现状逐字节一致
- [ ] 存量 SHA-256 token 在启用 pepper 后仍可认证（回退路径），并产生慢日志轮换提示
- [ ] 回退比对每 token 每窗口至多一次（不放大 DB 查询）
- [ ] 认证缓存、吊销即时生效语义回归通过（invalidate 广播路径不受影响）
- [ ] `.agents/skills/senv-cli/SKILL.md` 同步 env；help 验证
