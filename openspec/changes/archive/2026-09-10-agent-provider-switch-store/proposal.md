## Why

driver `agent-provider-switch-driver` 的第二个子 change：切换能力需要一个可跨机同步的 LLM Provider 档案库。凭据必须只存 vault（D4），档案只存引用；添加时要能从模型目录缓存自动加载模型集并支持自定义模型（D5）。

## What Changes

- 新增 `llm_providers/` 加密存储集合（类型化 entry，照 SSH 资产范式）
- 新增 `internal/llm` Provider 管理器：add/list/show/remove 与目录模型集装配
- 新增 CLI：`senv ai provider add/list/show/remove`（需解锁 vault）
- 凭据存 text 组 `llm-keys`，档案存引用；`--key-ref` 可指向既有 env/text entry

## Non-goals

- 不做 agent 配置写入与切换（agents 子 change）
- 不做凭据解密代取与代理转发
- 不做 provider 档案编辑器交互（用 remove+add 或 --force 覆盖）

## Capabilities

### New Capabilities

- `llm-provider`: LLM Provider 档案的加密存储、凭据引用与模型集管理

### Modified Capabilities

（无）

## Impact

- `internal/storage`：新增 `LLMProviderEntry` 与 `llm_providers/` 读写；通用 entry helper 去前缀化（错误文案调整，行为不变）
- `internal/llm`：新增 Provider 管理器与 `ProviderModelIDs`
- `internal/session`：新增审计事件 `op_llm_provider`
- `cmd/ai_provider.go` 新命令；需解锁 vault，写入走既有 vault mutation 锁

## 验证记录

- 2026-09-09：`go test -race ./internal/storage ./internal/llm ./cmd`（存储回环/校验、管理器装配与凭据归宿、CLI 全链路）全部通过；`make check`（fmt + vet + lint + go test -race ./...）全部通过。
