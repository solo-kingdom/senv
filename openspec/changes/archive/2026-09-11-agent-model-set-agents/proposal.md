## Why

driver `agent-model-set-driver` 的第二个子 change。store 已让指针与切换内核承载 Agent 模型集，但五个 agent 适配器仍只写单个模型（claude-code 的 `model`、codex 的 `model`、kimi 的单条 `[models.*]`、pi 的单元素 `models[]`、opencode 的单个 `models` 键），provider 的模型集在切换后仍然不可用。本子 change 把模型集按各 agent 原生机制写进配置，使 agent 自己的模型选择器能在集合内切换，并清理上一次写入的痕迹（ADR-0011/0012）。

## What Changes

- claude-code：`settings.json` 增加 `modelPicker`（每个选中模型一行，`replaceBuiltInOptions: true`），`model` 仍写默认模型
- codex：生成/更新 `~/.codex/model-catalogs/senv-<alias>.json` 并把 `model_catalog_json` 指向它；每条模型合成 codex 必填元数据，能取目录真实值的优先取真实值
- kimi：`default_model` + 每个选中模型一条 `[models."senv-<alias>/<m>"]`；`max_context_size` 取目录 `limit.context`，缺失回退保守值
- pi：`providers.<id>.models[]` 写全部选中模型；opencode：`provider.<id>.models{}` 写全部选中模型
- 清理：按指针中的上一次 Agent 模型集与本次集求差，删掉 `senv-<alias>` 命名空间里不再需要的条目，并删除不再被指向的 `senv-*.json` catalog；用户自有键与自有文件不动
- 事务：本次新建的 catalog 文件与删除动作纳入同一事务，失败一并回滚

## Non-goals

- 不改命令参数表面与 `senv ai status` 展示（cli 子 change）
- 不改 TUI（tui 子 change）
- 不改指针结构与 v1 兼容（store 子 change 已定）
- 不改接入地址归一与凭据落地方式（ADR-0002/0004/0006 不变）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider-switch`: 切换写回改为按 agent 原生机制投影 Agent 模型集与默认模型，并清理上一次 senv 写入的痕迹；事务覆盖新建 catalog 文件与删除动作

## Impact

- `internal/llm/switch.go`：五个适配器的 `Apply` 与事务路径
- `internal/llm/catalog.go`：读取目录中的模型元数据（`limit.context`、`reasoning_options`、`name`、`description`）
- `internal/llm/transaction.go`：支持新建与删除文件并参与回滚
- 测试：各适配器列表写回与幂等、清理差集、codex catalog 解析回环、事务回滚
- 文档：`.agents/skills/senv-cli/SKILL.md` 的切换语义（与 cli 子 change 协同）

## 安全性分析

- 本次新增删除动作，边界必须可判定：只删 `senv-<alias>` 命名空间内的条目与 `senv-*.json` catalog 文件，用户自有条目与文件既不改也不删；删除纳入事务，失败回滚。
- 新增的 codex catalog 文件不含任何凭据材料（只有模型 id 与公开元数据），写入仍收敛为 0600，与既有「明文只落在 agent 配置」的边界（ADR-0002）不冲突、不扩大。
- 不新增网络请求；目录元数据只读本地缓存，缓存缺失时回退模板而不是去拉取。

## 验证记录

- 2026-09-11：`go test ./internal/llm/...` 通过，新增 `modelmeta_test.go`（目录元数据命中/缺缓存/缺字段/损坏回退）、`modelset_projection_test.go`（五个 agent 的模型集写回、默认模型、幂等；缩集与换 provider 清理；用户自有条目保留；失效 catalog 删除与被其它 agent 引用时保留；新建 catalog 后配置失败整体回滚）与 `transaction_test.go` 的 remove/新建回滚用例。
- 2026-09-11：codex catalog 回环以真实 `codex debug models`（codex-cli 0.153.4）验证：`TestCodexCatalogParsedByCodexBinary` 解析回环通过；必填字段清单由实测确定（`supported_reasoning_levels`/`shell_type`/`visibility`/`supported_in_api`/`priority`/`support_verbosity`/`truncation_policy`/`experimental_supported_tools` + `base_instructions` 或 `model_messages.instructions_template`）。
- 2026-09-11：`make check`（fmt + vet + lint + go test -race ./...）全绿；`openspec validate agent-model-set-agents --strict` 通过。
