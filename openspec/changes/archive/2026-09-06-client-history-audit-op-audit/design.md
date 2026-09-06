## Context
internal/session/audit.go 已有 JSON-lines 追加写（~/.log/senv/audit.log，0600/目录 0700）与事件模型（session_start/expire/clear/validate、mcp_session_revoked、auth_success/auth_failure，见 internal/session/types.go:34-42）；业务写命令集中在 cmd/env.go、cmd/text.go、cmd/config.go，同步在 cmd/sync.go 与 cmd/autosync.go，冲突解决在 internal/provider/resolution.go。决策依据：driver grill.md（D5、D7）。

## Goals / Non-Goals
**Goals:** 业务操作事件落审计文件（不含值）、best-effort 写入、`senv audit` 与 TUI 审计 Tab 查看（含日期）
**Non-Goals:** 审计同步/跨机合并、记录值、server 侧日志、审计轮转清理（记为已知限制）

## Decisions
1. **扩展现有 audit.go 而非新建模块**：沿用 JSON-lines 文件、追加写与权限规范，新增业务事件类型（op_env/op_text/op_config/op_install/op_uninstall/op_sync/op_conflict），事件结构统一为 {time, type, target, outcome, detail}。备选「独立 operation-audit.log」会割裂查看入口，弃。
2. **埋点收口在命令层**：在 cmd/ 各写命令与 autosync/冲突解决的完成点调用统一 helper（记录成功/失败与目标），不在 storage/provider 层埋（避免自动同步放大噪音、避免重复记录）。
3. **best-effort 包装**：helper 内吞掉写错误并输出一次告警（stderr），不改变命令退出码。
4. **目标标识脱敏**：target 只到 kind/group/key 与文件名粒度；detail 仅放错误类别等非敏感摘要。

## 数据流
```
cmd 写命令/autosync/冲突解决 ──▶ audit helper ──▶ ~/.log/senv/audit.log (JSON lines, 追加)
senv audit --since/--type ──▶ 读文件倒序过滤 ──▶ 表格输出（含日期时间）
TUI 审计 Tab ──▶ 同一读取器 ──▶ 时间线浏览（过滤/翻页）
```

## 错误处理策略
- 审计文件缺失：首次写入自动创建（0600/0700）；只读查看时提示「暂无审计事件」
- 写入失败：stderr 一次告警，业务照常
- 事件行损坏（手工编辑）：查看器跳过坏行并在末尾提示跳过数量，不中断

## CLI 使用示例
```
senv audit                          # 最近事件（新到旧，含日期时间）
senv audit --since 2026-09-01 --until 2026-09-06
senv audit --type sync --limit 50
# TUI：Tab 栏新增 Audit，↑/↓ 浏览、f 切换类型过滤、PgUp/PgDn 翻页
```

## Risks / Trade-offs
- [审计文件无限增长] → JSON lines 单行小；轮转/prune 留作后续（Non-goal），风险已知
- [埋点遗漏] → tasks 按命令逐一列出验收；helper 单测覆盖各事件类型
- [多进程并发追加交错] → 沿用现有单行追加写（O_APPEND），单行原子性足够

## Migration Plan
纯新增事件类型，旧文件与旧事件不受影响；无迁移。回滚即回退二进制。

## Open Questions
无
