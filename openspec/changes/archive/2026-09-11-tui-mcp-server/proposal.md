## Why

MCP Server 档案与导出已有完整 CLI，但 TUI 没有对应 Tab。用户要改档案或看各 agent 的导出状态，只能退出 TUI 跑命令再回来，打断操作流。ADR-0005 要求 CLI 能做的数据编辑 TUI 也能做；当时 D9 把 TUI 整段延后，现在补上。

## What Changes

- 新增独立 MCP Tab（AI 之后）：左栏档案、右栏全部导出目标 agent（`agentcfg.Supported()`，含 claude-desktop/cursor）及其对当前档案的导出状态
- 档案 CRUD：`n` 新建、`e` 编辑（别名只读）、`d` 删除（不自动撤回）、`enter` 详情
- 导出/撤回：`x`/`u` = 当前档案 × 当前 agent，`X`/`U` = 当前档案 × 全部 agent；先出计划页再写盘；漂移默认 skip，`F` 才覆盖；撤回被改过的条目逐条 `y/n`
- 全局搜索 `S` 匹配档案 alias/command；写操作记 `op_mcp_server` / `op_mcp_export`，不含值
- 同步 `.agents/skills/senv-cli/SKILL.md` 的 TUI 键位说明

## Non-goals

- TUI 不做 `mcp install` / `serve` / `list-tools`
- 不做 `--print`、`--scope project`、非 stdio 传输
- 不做「全部档案 → 某 agent」批处理（继续走 CLI `senv mcp export --agent …`）
- 不改别名、不在删除档案时自动撤回
- 不把 `mcp_servers` 纳入 server provider 同步清单（沿用既有边界）

## 安全性分析

列表/详情/计划不渲染 env 字面量，只标键名与「明文 env」及目标路径。用户在表单里显式打开 `$EDITOR` 编辑 env 时，编辑器是 TUI 内唯一解密面（对标 Text/Config 的 `e`，不对标 AI 凭据字段）。导出仍按 ADR-0008 把解析后的值明文写入 agent 全局配置（0600 + `.bak`）；计划确认后才落盘。审计 target 只含 alias/agent id。

## Capabilities

### New Capabilities

- `mcp-server-tui`: MCP Tab 的两栏布局、档案 CRUD、导出/撤回计划确认、env 解密面与空态

### Modified Capabilities

- `tui-viewer`: Tab 注册顺序纳入 MCP；全局搜索覆盖档案 alias/command
- `tui-forms`: MCP 档案表单（args/env 走编辑器闭环）
- `operation-audit`: TUI 内 MCP 写操作与 CLI 记同一类事件

## Impact

`internal/tui`（新 `mcp_tab.go`、`Managers` 注入、搜索/Tab 顺序）、`cmd/tui.go`（装配 `internal/mcp.Manager` 与 Exporter）、复用既有 `mcp.Manager` / `mcp.Exporter`（不改 vault schema）。无新增依赖。
