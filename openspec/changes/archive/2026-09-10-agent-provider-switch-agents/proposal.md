## Why

driver `agent-provider-switch-driver` 的第三个子 change：store 已提供跨机同步的 provider 档案库，但用户仍需手工修改各 coding agent 的配置文件才能切换。本 change 把切换动作产品化：senv 维护每 agent 的单向指针，切换时按 agent 原生格式原子写回配置（D2/D3/D7/D8/D11）。

## What Changes

- 新增 agent 注册表与配置适配器：claude-code、codex、zcode、kimi、pi、opencode（cursor 明确不支持）
- 新增本机指针存储 `~/.config/senv/agent-pointers.json`：每 agent 记录 `(provider, model)`，不进 vault 不同步（D7/D11）
- 新增切换管理器：校验 → 解密凭据 → 适配器原子写配置 → 更新指针，失败不留半写状态
- 新增 CLI：`senv ai switch <agent> <provider> [--model]` 与 `senv ai status`

## Non-goals

- 不做 TUI 与 MCP 查询（tui/mcp 子 change）
- 不做 project scope 与多 profile（仅 user 级，D8）
- 不回读 agent 配置推断状态（D7 单向指针）
- 不支持 cursor（配置格式无法覆盖，status 明示不支持，D2）

## Capabilities

### New Capabilities

- `llm-provider-switch`: coding agent 注册表、本机指针存储、按 agent 原生格式的原子切换写回与状态查询

### Modified Capabilities

（无）

## Impact

- `internal/llm`：新增 pointer 存储与 SwitchManager、各 agent 适配器（JSON/TOML merge 写回）
- `cmd/ai_switch.go`：`senv ai switch` / `senv ai status` 子命令；switch 需解锁 vault
- 测试：tmp HOME 下适配器回环、指针存储、切换回滚与 CLI 全链路

## 验证记录

- 2026-09-10：`go test -race ./internal/llm ./cmd`（指针存储、适配器 merge/原子写/回滚、SwitchManager 全链路、CLI 全链路）全部通过；`make check`（fmt + vet + lint + go test -race ./...）全部通过；`openspec validate agent-provider-switch-agents --strict` 通过。
