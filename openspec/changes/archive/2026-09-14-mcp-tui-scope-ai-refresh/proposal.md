# Proposal: mcp-tui-scope-ai-refresh

## Why

能力审计发现两处 TUI 与 CLI 能力错位：**B5** — CLI `senv mcp export --scope project|user` 已支持 project 级导出，但 TUI MCP Tab 的导出 scope 硬编码 `user`（`internal/tui/mcp_tab.go:266`），project 不可达；**A4** — CLI `senv ai refresh` 可联网刷新 models.dev 目录缓存，TUI AI Tab 的 `ctrl+r` 只做本地档案重载，而 provider 表单的模型候选依赖该缓存，TUI 内无刷新入口。

## What Changes

- MCP Tab 新增 `s` 键切换导出 scope（user ↔ project，默认 user，会话内有效）；右栏状态列、`x/X` 导出计划、`u/U` 撤回计划全部按当前 scope 计算；右栏标题显示当前 scope
- AI Tab 新增 `R` 键：异步联网刷新模型目录缓存，复用 `internal/llm` 的 `Fetch`/`Save`（语义与 `senv ai refresh` 一致：先校验后原子替换，失败保留旧缓存），成功 toast 摘要并重载，失败红色 toast
- 两个 Tab 的 keyactions/help 与 `.agents/skills/senv-cli/SKILL.md` 同步

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `mcp-server-export`: 「导出目标选择」requirement 增加 scope 语义（CLI `--scope` 正式入规约 + TUI `s` 键切换、默认 user）；「撤回导出」requirement 增加 TUI 撤回与导出同 scope 的语义
- `llm-model-catalog`: 新增「TUI 目录刷新入口」requirement（`R` 键、异步刷新、失败保旧缓存、与 `ctrl+r` 本地重载并存）

## Impact

- `internal/tui/mcp_tab.go`（scope 字段、`s` 键、右栏标题、`exporter()` 消费 scope）
- `internal/tui/ai_tab.go`（`R` 键、异步 refresh Cmd、toast、Bindings、空态提示文案）
- `.agents/skills/senv-cli/SKILL.md`（两个 Tab 的按键说明）
- 纯消费 `internal/mcp`、`internal/llm`，无新依赖；台账 `mcp-exports.json` 与目录缓存格式均不变

## Non-goals

- 台账 `mcp-exports.json` 结构改造（增加 scope 维度）——现状与 CLI 一致，属另立项的存储变更
- scope 持久化（会话内有效，重启回 user，与 CLI 每次调用默认 user 一致）
- TUI 刷新目录的 `--source` 覆盖入口（TUI 用默认源）
- TUI 展示目录缓存元信息（`senv ai catalog status` 的 TUI 版另议）
- 改动 CLI 任何行为（本 change 纯消费方）

## 安全性分析

两项变更都不触碰 vault 加密面。MCP 导出/撤回沿用既有 `Exporter`（先计划确认后写入、0600 + `.bak` 备份、台账原子写），scope 只改变目标文件路径解析；唯一新增暴露面是 project scope 会把条目写入 CWD 相对的 `.cursor/mcp.json`，但计划弹窗逐条列出目标路径、确认前可见，与 CLI 同一透明机制。AI 刷新只读写公开的 models.dev 缓存（0600、temp+rename 原子替换、32MiB 上限），不接触任何密钥与 vault，失败路径旧缓存原样保留。

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 单仓变更 |

## 验收标准

- [ ] MCP Tab 按 `s` 在 user/project 间切换 scope，右栏标题显示当前 scope，状态列按新 scope 重算
- [ ] scope=project 时 `x/X` 导出计划对 cursor 指向 `.cursor/mcp.json`，其余 agent 路径与 user scope 相同；`u/U` 撤回同 scope
- [ ] 未切换时行为与现状完全一致（默认 user）
- [ ] AI Tab 按 `R` 异步刷新目录缓存，成功 toast 含 provider/model 计数并重载；失败红色 toast 且旧缓存字节不变
- [ ] `ctrl+r` 仍是纯本地重载（不发 HTTP）
- [ ] `go test ./internal/tui/ -race` 全绿；SKILL.md 按键说明已同步
