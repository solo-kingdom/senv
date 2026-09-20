# 0028-codex-responses-only-wire

Codex CLI 上游已移除 chat 线协议支持（openai/codex#7782：`wire_api = "chat"` 不再被接受，配置加载直接失败），senv 写给 codex 的 `wire_api` 钉死为 `responses`，`api_shape` 声明不再参与 codex 的线协议选择；codex 的 OpenAI 族接入地址随之固定取 `responses_base_url`（未设时按 ADR-0004 从 `BaseURL` 推断），不再取 `chat_base_url`。门禁相应收紧：档案显式声明 `openai-chat` 且无 `responses_base_url` 时拒绝切换 codex，错误文案给三个动作（补 `--shape-url openai-responses=<url>` / 改档案形态 / 换 provider），不写任何文件——此时写出的配置在 codex 侧必然不可用，fail-closed 优于写坏；设有 `responses_base_url` 视为档案同时服务 Responses 形态（ADR-0027 的存在性声明），照常放行。kimi/pi/opencode 仍按声明在 chat/responses 间选择，不受影响。

## Considered Options

- **继续为声明 `openai-chat` 的档案写 `chat`**：codex 拒绝加载整个配置，故障延后到用户启动会话时才暴露。放弃。
- **对 chat-only 档案静默写 `responses` 并回落推断地址**：写出的配置指向只讲 Chat Completions 的端点，运行时 404，正是本次要消除的「写坏配置」失败模式。放弃。
- **只改文档不改门禁**：与 ADR-0006「拒绝写入而不是把配置写坏」的既有判据相悖。放弃。

## Consequences

- 修订 ADR-0006 的投影表：codex 不再是「声明 `openai-chat` 时写 `chat`」的落点，`api_shape` 在 OpenAI 族内的线协议选择只剩 kimi/pi/opencode 三个消费方。
- `senv ai switch codex` 新增一种失败模式（chat-only 档案无 responses 地址）；存量被 codex 拒绝加载的配置可通过补 `responses_base_url` 或清除 `api_shape` 声明恢复。
- 上游若恢复 chat 支持，本决策需重新评估；以 upstream discussion #7782 为准。

## Status

采纳（2026-09-20）。修订 [ADR-0006](./0006-provider-api-shape.md) 的线协议投影，沿用 [ADR-0027](./0027-provider-per-shape-urls.md) 的形态地址存在性语义。
