## Context

五个适配器现在各自只写一个模型（`internal/llm/switch.go`）：claude-code 写顶层 `model`，codex 写 `model`/`model_provider`，kimi 写 `default_model` 加一条 `[models."senv-<alias>/<m>"]`，pi 写 `providers.<id>.models` 单元素数组，opencode 写 `provider.<id>.models` 单键。codex 的 `model_catalog_json` 不在其公开文档里，必填字段由 `codex debug models` 实测确定；models.dev 目录以 `json.RawMessage` 透传，含每模型 `limit`、`reasoning_options`、`name`、`description`。相关决策见 ADR-0011、ADR-0012 与 `grill.md` D1/D3/D5/D6/D7/D10。

## Goals / Non-Goals

**Goals:** 五个 agent 都能在自己的选择器里切换 Agent 模型集；切换幂等；清理只动 senv 命名空间；新建与删除纳入事务

**Non-Goals:** 命令参数表面与 status 展示（cli）、TUI 交互（tui）、指针结构与 v1 兼容（store）、接入地址归一与凭据落地方式

## Decisions

1. **codex 走生成的 catalog 文件**（D6）。备选：`[profiles.*]`（只能启动时用 `--profile` 切换，不是会话内选择器）、只写默认模型（丢失本子 change 的核心能力）。
2. **codex 元数据合成优先级**（D7）：`slug`/`display_name`/`description` 取目录值；`supported_reasoning_levels` 由目录 `reasoning_options`（effort）映射；`limit.context`/`limit.output` 映射 context 相关字段；`shell_type`、`truncation_policy`、`supports_parallel_tool_calls`、`experimental_supported_tools`、`base_instructions` 用固定模板；目录缺失任意字段时回退模板。写入前做一次自校验（必填字段齐全 + JSON 可解析），不通过即回滚。
3. **claude-code 用 `modelPicker` 且 `replaceBuiltInOptions: true`**（D5）：provider 是自定义接入地址，内置 lineup 打过来必失败。
4. **kimi 每条模型一条 `[models.*]`**，`max_context_size` 取目录 `limit.context`，缺失回退 131072；pi/opencode 只写 id/name，不引入额外元数据。
5. **清理算法**：以指针中的上一次 Agent 模型集与本次集求差；对差集里的模型删除 `senv-<alias>` 命名空间条目；若无其他 agent 仍指向该 provider 的本机记录，则删除对应 `senv-<alias>.json`；目标不存在时静默通过（幂等）。**只按 `senv-<alias>` 前缀匹配**，用户自有条目与文件既不改也不删。
6. **事务扩展**：`configTransaction` 增加「新建文件」与「删除文件/条目」两类记录，回滚时新建文件删除、删除内容写回；`model_catalog_json` 这类用户自有键被覆盖后，原值随备份恢复。

## 数据流

```
SwitchRequest{provider, models[], defaultModel, baseURL, credential, 上一次 models[]}
        │
        ├─ claude-code : settings.json  ← modelPicker(options[], replaceBuiltInOptions) + model + env
        ├─ codex       : config.toml    ← model + model_provider + model_catalog_json
        │                model-catalogs/senv-<alias>.json ← 选中模型 + 合成元数据（新文件，纳入事务）
        ├─ kimi        : config.toml    ← default_model + [providers.<id>] + 每模型 [models."<id>/<m>"]
        ├─ pi          : models.json    ← providers.<id>.models[] ; settings.json ← defaultProvider/defaultModel
        └─ opencode    : opencode.json  ← provider.<id>.models{} + 顶层 model
        │
        └─ 清理：差集模型条目 + 不再被指向的 senv-*.json（同一事务）
```

## 错误处理策略

- 任一写入、新建或删除失败 → 整体回滚（含新建文件删除、删除内容恢复），指针不更新，命令非 0 退出；错误信息保留主因
- codex catalog 自校验失败 → 视同写入失败回滚，错误必须说明缺失或非法的字段
- 目录缓存缺失或过期 → 元数据回退模板，不因此失败；仅在需要真实值而不可得时按模板写入
- 清理目标不存在 → 静默通过，不算失败（保证重复切换幂等）

## Risks / Trade-offs

- [codex 未公开 schema 变动导致写入即失败] → 写入前自校验；把 codex 解析失败的真实错误原样上报；本机 codex 解析回环作为任务验证步骤
- [清理误删用户内容] → 仅 `senv-<alias>` 前缀匹配 + 差集限定 + 单测覆盖
- [全集写入让 agent 配置明显变大] → 属 D3 的明确取舍；输出条数并在超过 20 个时提示可用 `--models` 缩小（cli 子 change 落实）
- [kimi 的保守 `max_context_size` 会低估真实上下文] → 优先取目录真实值，回退值取保守下限

## Migration Plan

无 vault 迁移。已切换过的 agent 保持原配置，下一次切换时补齐模型集与清理旧条目。回滚：还原二进制即可；已有配置多出的模型条目对 agent 无害，如需恢复单模型形态重跑一次旧版切换。

## Open Questions

无
