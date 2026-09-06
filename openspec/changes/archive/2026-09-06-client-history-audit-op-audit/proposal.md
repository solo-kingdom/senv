## Why
client 业务操作（env/text/config 增删改、安装卸载、同步、冲突解决）目前无任何记录，出问题无法回答「何时改了什么」；现有 audit.log 仅记 session/MCP 认证事件。需扩展为业务操作审计并可经 cli+tui 查看（含日期）。

## What Changes
- 扩展 internal/session/audit.go 事件模型：新增业务操作事件（env/text/config 增删改、config 安装/卸载、同步 push/pull、冲突解决），记录操作、目标、日期时间、结果，**不记值**
- 审计写入 best-effort：写入失败不阻断业务操作，仅告警
- 新增 `senv audit` 命令：按日期（--since/--until）与操作类型过滤查看
- TUI 新增审计 Tab：时间线浏览（含日期时间）
- 仅本机存储；文件/目录权限维持 0600/0700

## Non-goals
- 审计随 vault 同步与跨机合并；记录值或明文差异；server 侧访问日志（属 access-log 子 change）

## Capabilities

### New Capabilities
- `operation-audit`: client 本机业务操作审计的记录内容、可靠性边界与 cli+tui 查看

### Modified Capabilities
（无——沿用现有 audit.log 文件与 JSON-lines 格式，仅新增事件类型）

## Impact
- client：internal/session/audit.go（事件模型）、cmd/ 各写命令（埋点）、新增 cmd/audit.go、internal/tui（新 Tab）
- 隐私：不记录值；文件权限沿用现有敏感文件规范（0600/0700）

## 安全性分析
- 审计事件仅含操作元数据（类型/目标/时间/结果），不含任何值或明文内容
- 写入 best-effort，失败只告警，不影响密钥操作的正确性
- 查看入口仅本机，文件权限与现有敏感文件规范一致

## 验证记录
- 2026-09-06：`make check` 全绿，含新增 session/audit 单测、cmd 埋点集成测试与 TUI audit tab 测试。
- 2026-09-06：闭环验证以自动化测试承担：`TestEnvSetDeleteWriteAuditEvents`（成功+失败留痕、target 正确、不含值）、`TestTextSetWritesAuditEvent`、`TestAuditViewerFiltersAndSkipsBadLines`（日期过滤语义、坏行跳过、查看器渲染 ✓/✗）、`TestLogOpBestEffort`（写失败不影响调用方）、TUI `TestAuditTab*`（渲染、过滤循环、跳行提示、错误态）。
- 范围补充（相对原 design 的微调，随 change 记录）：新增 `op_restore` 事件类型用于历史恢复（entry-history 子 change 引入的新写操作），并在 cmd/history.go 恢复路径埋点。
- 埋点位置：env set/delete、text set（file/stdin/arg/editor 各路径）/delete/group-delete、config create/delete、config install/uninstall、`senv sync`（git + server 的 accept-remote/force-push/双向/冲突）、autosync（仅实际发生数据变更或失败时记录，避免噪音）、关键写入阻塞推送、交互式冲突解决、历史恢复。
- 术语澄清：audit 文件路径定位 `session.AuditLogPath()`（HOME/.log/senv/audit.log）。
