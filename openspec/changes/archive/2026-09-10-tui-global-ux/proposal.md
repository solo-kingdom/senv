## Why

TUI 已注册 7 个 Tab，但数字键硬编码只认 `1`–`5`（`internal/tui/model.go`），History/Audit 无法直达；全局搜索只覆盖 env/text/config；底部帮助是单行硬截断，没有键位总览；操作反馈分三套（env/text 的 per-tab flash、AI 的 notice、顶部错误横幅）；长行依赖 lipgloss `Width` 硬换行，把面板撑坏。另有两个信任缺口：TUI 写操作不进操作审计（`auditOp` 只在 `cmd/` 被调用），同步状态不可见（`postRunAutoPush` 只在退出 TUI 后才跑，失败提示打印在 alt-screen 关闭之后）。

## What Changes

- 数字键 `1`–`9` 动态映射已注册 Tab；`Tab`/`Shift+Tab` 循环语义不变。
- 全局搜索 `S` 覆盖 SSH host 与 LLM provider（仍只匹配标识，绝不匹配值）。
- 新增 `?` 键位总览 overlay：当前 Tab 键位 + 全局键。
- 统一反馈：所有 Tab 的操作结果走同一条底部提示条（错误 > 警告 > 成功），成功提示超时自动消失。
- 硬规则：面板内容不依赖 `Width` 换行，一律截断 + `enter` 看详情。
- TUI 写操作落操作审计，Audit Tab 可见。
- Footer 常驻同步状态（待推送条数/上次同步）；写后异步 auto push（沿用 2s 预算）；退出前仍有 dirty 给一次提示。
- 面向用户的新文案统一简体中文，键位名与技术标识保留原文。

## Non-goals

- 不改 CLI 输出与审计文件格式。
- 不在 TUI 内做冲突解决（仍走 `senv sync`）。
- 不引入 undo、多选、批量操作。
- 不改 session/密码流程与最小尺寸降级行为。

## Capabilities

### Modified Capabilities

- `tui-viewer`: Tab 切换、全局搜索覆盖、键位总览、统一反馈、截断与详情、同步状态可见性、文案语言。
- `operation-audit`: TUI 写路径也追加业务操作事件；审计 Tab 支持自由文本过滤。

## Impact

`internal/tui`：`model.go`（数字键、提示条、overlay 路由）、`search.go`（新增两类数据源）、`list.go`（截断硬规则）、各 tab 的反馈接入与 `?` 帮助；新增审计写入与同步状态适配。`cmd/tui.go` 注入审计与同步源。无 vault 格式变更，无新增依赖。
