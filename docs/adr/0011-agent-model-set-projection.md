# 0011-agent-model-set-projection

`senv ai switch` 写的不是一个模型，而是 Agent 模型集（Provider 模型集的子集，默认全选）加一个默认模型；senv 按每个 Coding Agent 的原生机制把这份集合投影进它的配置，使 agent 自己的模型选择器就能在集合内切换：claude-code 写 `settings.json` 的 `modelPicker.options`（`replaceBuiltInOptions: true`，只列 senv 的模型，避免内置 lineup 打到自定义接入地址）、codex 写 `model_catalog_json` 指向 senv 生成的 `~/.codex/model-catalogs/senv-<alias>.json`、kimi 写多条 `[models."senv-<alias>/<model>"]`、pi 写 `providers.<id>.models[]`、opencode 写 `provider.<id>.models{}`。理由是「切换」在用户心智里是换 provider 而不是换一个模型：只写一个模型会让 agent 内的模型选择器失去意义，换模型必须回到 senv。代价是 senv 要合成 agent 专属元数据——codex 的 catalog 每条模型必须有 `supported_reasoning_levels`、`shell_type`、`truncation_policy`、`base_instructions` 等十余个字段（实测缺失即解析失败），其中能用 models.dev 目录填的用真实值（`limit.context` → kimi 的 `max_context_size`，`reasoning_options` → codex 的 reasoning levels），填不上的用固定模板。

## Considered Options

- **只写默认模型（原实现）**：agent 配置最小，但 agent 内无法切换，多模型 provider 的模型集形同虚设。
- **codex 走 `[profiles.<name>]` + `--profile`**：不用合成元数据，但那是启动参数切换、不是会话内的模型选择器，无法表达「在 agent 里换模型」。
- **codex 降级为单模型**：避开 `model_catalog_json` 这个未公开 schema，但 codex 恰好是与 senv 最贴合的族（responses + env_key），降级面太大。

## Consequences

- `model_catalog_json` 不在 codex 公开文档里，必填集由 `codex debug models` 实测确定；codex 升级可能改变它，属已知脆点，写入前需能报告真实错误。
- senv 合成的 `base_instructions` 是通用模板，不等同于用户手写 catalog 里的定制文案。
- Agent 模型集必须非空；`--models` 省略即全选，显式给定则必须是 Provider 模型集的子集。
- 这些写入只在 senv 自己的命名空间内进行，并在切换时清理旧痕迹（见 [ADR-0012](./0012-senv-owns-isolated-agent-config-namespace.md)）。

## Status

proposed
