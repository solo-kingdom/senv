## Why

tui-viewer spec 要求「每个 Tab SHALL 支持 `/` 过滤」，但 SSH/AI/MCP 三个 Tab 没有实现；且过滤输入状态机（append/backspace/esc/enter）在 env/text/config/audit 复制了 4 份。grill D4 已定补齐与共享组件方向。

## What Changes

- 新建共享过滤组件（输入状态机 + 与列表组件挂接），匹配统一走 `matchKey` 标识符子串、大小写不敏感、不碰值
- `/` 过滤接入 SSH/AI/MCP：作用于左栏主列表，右栏联动显示当前选中项
- env/text/config/audit 迁移到共享实现，行为不变（config 侧栏计数语义由 config-tui spec 约束，保持）
- audit 的 `f` 预设过滤循环保留，作为共享组件的扩展点

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: 「Tab 内过滤」明确各布局的作用范围（单栏=主列表；SSH/AI/MCP 双栏=左栏主列表；Config 见 config-tui；audit 预设保留）

## Impact

- 代码：`internal/tui/filter.go`（新增）、`ssh_tab.go`、`ai_tab.go`、`mcp_tab.go`、`env_tab.go`、`text_tab.go`、`config_tab.go`、`audit_tab.go`
- 文档：`.agents/skills/senv-cli/SKILL.md`

## Non-goals

- 不做 fuzzy、不扩大匹配范围到 value、不改全局 `S`（grill D4）
- 不动 config 侧栏计数行为、不引入多选（⑤）

## 验证记录
- 2026-09-11（分支 tui-ux）：`Filter` 状态机落地 `filter.go`（Enter/EnterFresh/Append/Backspace/Confirm/Clear/Matches/Prompt），匹配统一走 matchKey；env/text/config 三处手写过滤迁移到组件（保留各自 `/` 清词进入语义），audit 自由文本迁状态机、匹配谓词保留 auditEntryMatches；SSH/AI/MCP 接入 `/`（过滤左栏主列表，右栏联动，focusJump 前清过滤保证目标可见），左栏标题显示过滤提示；新增单测（状态机 + 三 Tab 集成）并同步既有测试；SKILL.md 补 SSH/AI/MCP `/` 说明；`make check` 全部通过。
