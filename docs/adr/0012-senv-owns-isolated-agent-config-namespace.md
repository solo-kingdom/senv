# 0012-senv-owns-isolated-agent-config-namespace

`senv ai switch` 只改 agent 配置里属于 senv 的部分：本次必须改的键（如 claude-code 的 `model`/`env`/`modelPicker`、codex 的 `model`/`model_provider`/`model_catalog_json`），以及 `senv-<alias>` 命名空间下的 provider 与模型条目；用户自己写的同名键（例如手写的 `model_catalog_json` 指向、非 senv 的 provider 块）不动、不记旧值，覆盖前仍按现有事务留 `<file>.senv-bak`。同时切换要清理自己上一次的痕迹：按「当前指向 + 本次 Agent 模型集」算差集，删掉本次不再需要的 senv 条目，并删除不再被指向的 `senv-*.json` catalog 文件。理由是单向模型下 senv 不回读 agent 配置，若不清理，A→B 切换后 agent 的模型选择器会同时挂着 A 与 B 的模型，缩小 `--models` 后旧模型也仍在，ADR-0011 的能力会被残留抵消；反之若为了「干净」把用户自己的条目也纳入清理，就把 senv 的管理边界扩到了整个文件。

## Considered Options

- **不清理（原实现）**：切换只覆盖当前 provider 的块，旧条目留在原地——kimi/pi/opencode 的模型选择器会越积越多。
- **清理一切非本次写入的条目**：选择器最干净，但会删掉用户自己加的 provider/model。
- **在 agent 配置里写 senv 标记来判定归属**：判定精确，但污染 agent 配置，且各家格式对未知键的容忍度不一致（同 [ADR-0007](./0007-mcp-export-source-of-truth.md) 的取舍）。

## Consequences

- 指针（`~/.config/senv/agent-pointers.json`）成为清理依据：它记录 Agent 模型集，因此是本机事实源；version 1 的旧指针（只有单个 `model`）读作单元素集，用户无需重新切换。
- 用户手工往 `senv-<alias>` 命名空间里加的东西会被下次切换清掉：命名空间前缀即所有权声明。
- 与 ADR-0007 同构：本机状态、单向写入、不回读 agent 配置。

## Status

proposed
