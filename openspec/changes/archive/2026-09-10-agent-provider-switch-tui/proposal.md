## Why

driver `agent-provider-switch-driver` 的第四个子 change：CLI 已具备 provider 管理与 `senv ai switch/status`，但浏览档案与切换仍需多条命令。按 D9/D10 在既有 `senv tui` 中新增 AI Tab：浏览 provider 档案与各 agent 当前指向，并直接完成切换操作。

## What Changes

- `tui.Managers` 新增 LLM 字段（`*llm.ProviderManager` + 切换管理器构造输入），沿用「nil 不注册 Tab」惯例
- 新增 `internal/tui/ai_tab.go`：双栏浏览（provider 列表 + 详情/指针区）
- Tab 内切换操作：选择 provider → 选择 agent → 选择模型 → 确认执行，成功即时刷新指针展示，失败走既有错误横幅
- `cmd/tui.go` 在 vault 已解锁时注入 LLM 管理器

## Non-goals

- 不做 MCP 只读查询（mcp 子 change）
- 不做 provider 档案增删改（仍走 CLI `senv ai provider`，D9 仅要求浏览+切换）
- 不显示凭据明文

## Capabilities

### New Capabilities

- `llm-provider-tui`: TUI 内的 LLM provider 浏览、当前指向展示与切换操作

### Modified Capabilities

（无）

## Impact

- `internal/tui`：新增 ai_tab 与测试；model.go 注册表与 Managers 扩展
- `cmd/tui.go`：构造并注入 ProviderManager/SwitchManager
- `README.md`：TUI mode 键位表补充 AI Tab

## 验证记录

- 2026-09-10：`go test -race ./internal/tui ./cmd`（Tab 注册/跳过、浏览渲染无凭据明文、切换成功刷新、失败错误横幅、codex 提示、esc 取消）全部通过；`make check`（fmt + vet + lint + go test -race ./...）全部通过；`openspec validate agent-provider-switch-tui --strict` 通过。
