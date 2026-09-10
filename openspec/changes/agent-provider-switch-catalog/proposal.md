## Why

LLM Provider 切换（driver `agent-provider-switch-driver`）需要一个 provider/model 目录作为添加档案时的模型来源。models.dev 提供公开目录（`https://models.dev/api.json`），需要拉取、校验并落盘缓存，保证离线可用。

## What Changes

- 新增 `senv ai` 命令分组（不需要解锁 vault）
- 新增 `senv ai refresh`：拉取 models.dev 目录，校验后原子写入 `~/.config/senv/cache/models-dev.json`（带格式版本与拉取时间）；支持 `--source` 覆盖目录地址
- 新增 `senv ai catalog status`：离线查看缓存元信息（拉取时间、provider/model 数量）
- 拉取或解析失败时保留旧缓存并明确报错

## Non-goals

- 不做 provider 档案存储与凭据管理（后续子 change）
- 不做自动定时刷新，仅显式 refresh；调用方（add）按缓存新鲜度自行提示
- 缓存不含任何敏感数据，不进 vault、不随同步分发

## Capabilities

### New Capabilities

- `llm-model-catalog`: models.dev 模型目录的拉取、校验、缓存与离线读取

### Modified Capabilities

（无）

## Impact

- 新增 `internal/llm` 包（目录类型、解析校验、缓存读写）与 `cmd/ai*.go` 命令
- 无既有行为变更；不引入第三方依赖（net/http + encoding/json）

## 验证记录

- 2026-09-09：`go test -race ./internal/llm ./cmd`（Fetch/Parse/Save/Load 与 CLI 三场景）全部通过；`make check`（fmt + vet + lint + go test -race ./...）全部通过。
