## Context

senv-server 现状见 proposal.md - Why；代码事实：`internal/server/store/accesslog.go`（`RecordAccess` 单条 INSERT，无防篡改）、`senv-server/main.go`（admin 子命令直写 store，无审计）、`internal/server/handler/handler.go`（token 认证走 `store.AuthenticateWithClient`，哈希方式在 store 层）。部署形态已确认：单实例、反代终结 TLS、双 DSN 运维（serve 用受限角色，admin/logs-prune 用高权限角色 + cron）。

## Goals / Non-Goals

**Goals:**
- 四项加固各自给出实现路径与验证手段：DB 角色隔离 + admin 审计、通用 webhook 告警、token HMAC pepper、部署加固文档
- 四项拆成可并行、范围不重叠的子 change，各自带 `tasks.md` 独立交付

**Non-Goals:**
- 见 proposal.md Non-goals（零知识架构不动；重防护项不做）

## Decisions

1. **DB 角色隔离的实现层**：新增迁移脚本输出说明 + 文档中的 GRANT 模板，不做代码强制。serve 进程只拿受限 DSN，代码无从区分——角色是部署期配置，代码层无切换点。备选「代码内按操作分连接池」被否：复杂且无收益。admin 操作审计则必须代码实现：admin 子命令成功后写入 `access_log`（`outcome` 新增 admin 类取值或复用 `reason` 字段），与请求事件同表，便于统一查询。

2. **告警检测的数据源**：基于现有 `access_log` 行做阈值扫描，而非请求路径内联 hook。检测器放在 serve 进程内（goroutine + ticker，或每次 `RecordAccess` 后内联检查），不落新表、不依赖 cron。备选「独立 daemon 读日志」被否：增加部署面。告警内容只含元数据（IP、client 名、事件类型），绝不带密文或 token。

3. **pepper 的分发与迁移**：pepper 走环境变量 `SENV_SERVER_TOKEN_PEPPER`（必需，serve 启动时校验非空？—— 不强制：允许空 pepper 保持旧行为，便于灰度），HMAC 用 `hmac.New(sha256.New(), pepper)`。迁移策略：store 层认证先查 HMAC 哈希，查不到回退到无 pepper 的 SHA-256 比对（一次性比对路径，标记慢日志提示轮换），旧 token 随 `revoke + create-user` 自然淘汰。备选「一次性全库重哈希」被否：需要明文 token，违背「明文只展示一次」的设计。

4. **部署文档的落点**：全部写入 `docs/senv-server.md` 已有「构建与发布」之后新增「公网加固」节；不新建文件，元数据泄露边界作为该节子段。

## Risks / Trade-offs

- [pepper 回退路径存在期是攻击窗口（DB 泄露可反查旧 token）] → 文档要求启用 pepper 后尽快轮换所有 token；慢日志标记供运维跟进
- [webhook 检测器在请求路径内联会增加单请求延迟] → 检测逻辑放 goroutine，事件经 channel 投递，阻塞不传染请求路径
- [admin 审计写 access_log 会让高权限操作依赖受限角色] → 审计写入复用 serve 已有连接（admin CLI 是独立进程，用 admin DSN 写，不冲突）
- [双 DSN 部署遗漏导致 logs-prune 失败] → `logs-prune` 失败时输出明确错误指向文档 GRANT 模板

## Migration Plan

四项相互独立，无强序；建议顺序：DB 角色隔离 + admin 审计（收益最大）→ webhook 告警 → pepper → 文档收尾。回退：角色隔离回退为单角色；webhook 配置清空即停；pepper 置空即回退旧行为（保留旧 token 反查兼容）。

## Open Questions

- 元数据泄露边界的文档子节具体措辞：实现期随文档写作定，不改方案
- webhook 重试策略（失败退避 / 死信）：实现期按最简单策略（丢弃 + slog 记录）起步
