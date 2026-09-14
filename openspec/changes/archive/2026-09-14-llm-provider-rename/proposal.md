## Why

`senv ai provider edit` 把 alias 当主键且不可改；用户改名只能「新建 + 删旧」，易漏联动自有凭据 `text:llm-keys/<alias>` 与本机 agent 指针。需要对齐 keypair rename：提供正式 rename，一次 mutation 内完成档案与必要引用的改名。

## What Changes

- 新增 `senv ai provider rename <old> <new>`；TUI AI Tab provider 栏增加 `r` 重命名。
- 成功时同次操作内：档案改名；若凭据为规范自有引用则同步改名并更新 `credential_ref`；更新本机 agent 指针中的 provider 字段。
- `edit` 仍禁止改别名；MCP 仍只读。
- 命令输出受影响指针数，并提示 coding agent 原生配置需重跑 `senv ai switch` 才会出现 `senv-<new>`。

## Non-goals

- 不自动改写各 coding agent 原生配置文件或 codex catalog 文件名。
- 不通过 `edit` 改名；不新增 MCP 写工具。
- 不迁移已落盘的 `SENV_<OLD>_API_KEY` 等 env 种子条目（重切 switch 时按新 alias 再播种）。

## 安全性分析

- 自有凭据改名只移动 vault 内已加密 text 条目，明文不进 argv/日志/TUI 渲染。
- 目标 alias 或目标 `llm-keys/<new>` 已存在时 fail-closed，不做部分写入。
- 外部 `--key-ref` 条目不移动、不删除。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider`: 新增 rename 要求；明确 edit 仍不可改 alias，以及自有凭据/指针联动与失败语义。
- `llm-provider-tui`: provider 栏增加 `r` 重命名入口与结果反馈。
- `llm-provider-switch`: 明确 rename 更新本机指针但不改写 agent 原生配置；漂移/重切语义。

## Impact

- CLI：`cmd/` ai provider 子命令；`.agents/skills/senv-cli/SKILL.md`、README。
- 域：`internal/llm` ProviderManager（及指针读写）、必要时 text 存储 rename。
- TUI：`internal/tui/ai_tab.go` 键位与表单。
- 测试：rename 成功/冲突/自有凭据/外部引用/指针联动。
