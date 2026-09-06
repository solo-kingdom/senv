## Why
server 目前只有 user 级匿名 token（admin 手工签发、手工拷贝），无法区分设备、无法按设备禁用。需要 client 注册与屏蔽的完整闭环：管理员可控接入新 client，设备失窃/流失时阻断访问，client 端感知被屏蔽后清理本机解锁缓存。

## What Changes
- server 新增 `clients`（设备身份）与 `registration_codes`（一次性注册码）表（0002 迁移），`tokens` 增加可空 `client_id`
- admin CLI 新增 create-registration / list-clients / block-client / unblock-client
- 新增注册端点：client 凭一次性注册码换取 client 专属 token（明文仅展示一次）
- 认证中间件：被屏蔽 client 返回 403 + `client_blocked`（无效/缺失/吊销仍 401）
- client 新增 `senv server register`；任一请求收到 `client_blocked` 时自动清理解锁缓存、保留本地加密数据并附重新注册指引

## Non-goals
- 被屏蔽 client 的 server 侧数据清理；审批队列式注册；开放自注册；Web 管理界面

## Capabilities

### New Capabilities
- `server-clients`: client 设备身份的注册、屏蔽/解封、被屏蔽访问行为与 client 侧感知清理

### Modified Capabilities
（无——`server-auth` 既有 requirement 与本变更不冲突：401 语义仅覆盖无效/缺失/已吊销 token，屏蔽是新增行为）

## Impact
- server：internal/server/store、internal/server/migrate（0002）、internal/server/handler（注册端点、中间件、错误映射）、senv-server/main.go（admin 子命令）
- client：internal/provider/server_client.go（403 映射）、internal/session（清缓存与审计事件）、cmd（register 命令）
- 协议：新增 403 语义与注册端点；旧 client 不调用新端点，行为不变

## 安全性分析
- 注册码一次性、带过期、仅存哈希；token 明文仅展示一次、库中仅存哈希（沿用现有机制）
- 屏蔽使该 client 名下 token 立即失效；`client_blocked` 仅对持有效凭证者可见，不泄露用户/vault 存在性
- 注册端点复用认证失败限速器
- 向后兼容：存量 user 级 token 保留（client_id 为空继续有效），不破坏现有部署

## 验证记录
- 2026-09-06：`make check`（fmt+vet+lint+test -race）全绿，含新增 store/handler/provider/cmd 测试。
- 2026-09-06：闭环验证以自动化 e2e 承担（强于手工）：`TestE2EClientRegistrationAndBlock` 覆盖 存量机器建 vault → 注册码注册 → 新机 bootstrap 接入 → 屏蔽 → client 感知清解锁缓存（审计留痕、加密数据保留）→ 机器 A 不受影响 → 解封恢复。
- 2026-09-06：admin CLI 真实 Postgres 冒烟通过：migrate（0002）/ create-user / create-registration / list-clients / block-client（ghost 报「不存在」）。
- 已知行为：Go flag 解析限制，admin 子命令的 --dsn 须置于位置参数之前（usage 文本已含示例）。
