## Why

同一份档案的 `base_url` 被原样写进 5 家 coding agent 的配置，但两族接入点形态不同：Anthropic Messages（claude-code）由 SDK 自行拼 `/v1/messages`，base 不能带版本段；OpenAI 兼容族（codex/kimi/pi/opencode）要求 base 带 `/v1`。实测 `https://token-api.wii.pub/v1` 写进 claude-code 会请求 `/v1/v1/messages`（404），而缺 `/v1` 的形态写进 opencode 会打到 HTML 首页。用户只能事后手工改配置，`ai switch` 不再是一次可靠动作。

## What Changes

- 档案接入地址在写入 vault 时归一为 OpenAI 兼容形态：补末段 `/v1`、收敛尾斜杠；已归一则静默通过，发生改写时提示。
- `ai switch` 按 agent 协议族转换后再写回配置：Anthropic 族剥离末段 `/v1`（无损变换），OpenAI 兼容族保持带版本形态。归一函数幂等，读写两侧共用。
- `ai switch` 输出实际写入的接入地址，让归一化的改写可见。
- 只处理路径末段，保留 query/fragment，不做协议探测。

## Non-goals

- 不新增 per-agent 接入地址覆盖字段，也不加 `--verbatim` 逃生舱。
- 不识别 `/v1beta` 等版本变体，不猜测非 `/v1` 的版本段。
- 不改 vault 档案 schema，不迁移存量档案，不自动改写其它工具写过的配置。
- 不改变 MCP 只读契约：展示落库值，不做 per-agent 转换。

## Capabilities

### Modified Capabilities

- `llm-provider`: add 时归一接入地址，发生改写时输出提示。
- `llm-provider-switch`: 按 agent 协议族写回接入地址，并输出实际形态。

## Impact

影响 `internal/llm`（归一函数、adapter 协议族、`Switch`、`AddProvider`）与 `cmd/ai_switch.go` 输出。无 vault schema 变更，无新增依赖。
