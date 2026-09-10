## Why

driver `agent-model-set-driver` 的第三个子 change。写回语义已能承载 Agent 模型集，但命令表面仍是单模型：`--model` 表达的是旧语义，`senv ai status` 只显示一个模型，模型集与漂移都不可见。参数不改会让用户继续按旧语义使用，也会让「默认全选」没有入口。

## What Changes

- `senv ai switch <agent> <provider> [--models m1,m2] [--default-model D]`：省略 `--models` 即全选 Provider 模型集；显式给出时保序
- 移除 `--model`：出现即报错并提示改用 `--models` 与 `--default-model`
- `senv ai status` 显示 `provider / 默认模型（N 个模型）`，指针模型集与档案模型集不一致时提示漂移（不读 agent 配置）
- 成功输出包含 provider、Agent 模型集条数与默认模型
- 审计 detail 由 `model:X` 改为 `default:X models:N`
- 同步 `.agents/skills/senv-cli/SKILL.md` 的切换与 status 说明

## Non-goals

- 不改写回内容、投影机制与清理算法（agents 子 change）
- 不改 TUI（tui 子 change）
- 不改指针结构与 v1 兼容（store 子 change）
- 不改 `senv ai provider` 系列命令

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider-switch`: 新增切换命令参数要求（`--models`/`--default-model`、拒绝 `--model`、输出与审计字段），并修改 status 展示（模型集条数与漂移）

## Impact

- `cmd/ai_switch.go`：flag 定义、参数校验、输出与审计 detail
- `cmd/ai_provider.go` 不动；`senv ai status` 的输出列
- 测试：flag 解析与错误路径、status 漂移展示
- 文档：`.agents/skills/senv-cli/SKILL.md`、README 相关命令示例

## 安全性分析

- 参数校验前置于解锁与写入：`--model` 等非法输入不触发解密、不触碰任何文件。
- 新输出与审计字段只含 provider 别名、默认模型与模型条数；MUST NOT 增加任何凭据或值的暴露面（沿用 operation-audit 的既有约束）。
- `senv ai status` 仍免解锁可用：漂移判定所需档案不可得时只省略提示，不回退去解析 agent 配置文件。

## 验证记录

- 2026-09-11：`go test ./cmd/...` 通过，新增/更新 `cmd/ai_switch_test.go`：`--models` 显式保序与 `--default-model` 覆盖本次且不回写档案、输出含默认模型与条数、审计 detail 为 `default:m2 models:2` 且不含凭据、六类参数错误（`--model` 已移除 / 空集 / 越界 / 默认模型不在集合 / 不支持 agent / 档案不存在）均在解锁与写盘前失败且不留文件、模型集超阈值提示。
- 2026-09-11：`senv ai status` 展示 `provider / 默认模型（N 个模型）`，漂移提示覆盖一致 / 档案缩集 / 档案不可得（`clearAuthMemo` 后只省略提示）三类；`--model` 已从 `--help` 隐藏并报错提示替代用法。
- 2026-09-11：`make check`（fmt + vet + lint + go test -race ./...）全绿；`openspec validate agent-model-set-cli --strict` 通过。
- 文档：`.agents/skills/senv-cli/SKILL.md`（1.5）与 README 的 switch/status 示例已同步。
