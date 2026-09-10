## Why

driver `agent-model-set-driver` 的第一个子 change。切换语义要从「provider + 单个模型」变成「provider + Agent 模型集 + 默认模型」，而本机指针是这套语义的事实源：不先把指针结构与兼容规则定下来，适配器和 CLI/TUI 都没有可依赖的状态形状。

## What Changes

- 指针结构：每 agent 记录 `provider` + `models[]` + `default_model` + `switched_at`（取代单 `model`）
- v1 兼容：只有 `model` 的旧指针读作 `models=[model]`、`default_model=model`，无需用户重新切换
- 切换内核：`SwitchRequest` 携带模型集与默认模型；校验「模型集非空、每个模型属于 Provider 模型集、默认模型属于模型集」
- 提供「上一次写入的 Agent 模型集」读取，作为 agents 子 change 清理差集的依据
- `cmd/ai_switch.go` 调用点适配编译（不引入新 flag）

## Non-goals

- 不改任何 agent 适配器的写回内容（agents 子 change）
- 不引入 `--models`/`--default-model`，不改 `--model` 行为（cli 子 change）
- 不改 `senv ai status` 展示（cli 子 change）
- 不迁移 vault 数据，不改 LLM Provider 档案 schema

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider-switch`: 本机指针存储记录 Agent 模型集与默认模型，并兼容 version 1 旧指针

## Impact

- `internal/llm/pointer.go`：结构、v1 归一读取、原子写入
- `internal/llm/switch.go`：`SwitchRequest`/`Switch` 入参与校验
- `cmd/ai_switch.go`：调用点编译适配
- 测试：指针 v1→新结构兼容、模型集与默认模型校验

## 验证记录

- 2026-09-11：`go test ./internal/llm/...`（指针 v1 兼容、Agent 模型集解析与六类校验、切换内核）通过；`go test ./cmd/... ./internal/tui/...` 通过；`make check`（fmt + vet + lint + go test -race ./...）全绿；`openspec validate agent-model-set-store --strict` 通过。
