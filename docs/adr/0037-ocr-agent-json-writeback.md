# Open Code Review（ocr）agent 支持：custom_providers 写回、多协议直通与 api_key_cmd 凭据引用

Open Code Review（`alibaba/open-code-review`，CLI 命令 `ocr`）把模型与供应商配置固定存储在 `~/.opencodereview/config.json`。它与 senv 既有单协议族 coding agent 有两处结构差异：一是同一配置面同时支持 `openai`、`openai-responses` 与 `anthropic` 三种协议；二是 `custom_providers.<name>` 的 `api_key` 为纯字面量（不展开 `$VAR` 且无环境变量回退），但原生提供 `api_key_cmd`（Unix 下走 `sh -c <cmd>` 取 stdout）。

## 决策

1. **作为独立 agent 注册，规范 ID `ocr`，别名 `open-code-review`**。展示名 `"Open Code Review"`，别名仅在 `LookupAgent` 接受，列表、报错、指针与状态展示面只出现 `ocr`（沿用 [ADR-0030](./0030-oh-my-pi-agent-yaml-writeback.md) / [ADR-0032](./0032-omo-agent-json-writeback.md)）。
2. **写 `~/.opencodereview/config.json` 的 `custom_providers["senv-<alias>"]` 与顶层 `provider`/`model`**。使用 `applyJSONMerge`（0600 原子替换）保留 `language`、`effort`、`max_tokens`、`telemetry`、`mcp_servers`、内置 `providers` 与用户自有 `custom_providers` 条目；整对象覆盖 `custom_providers[id]` 以确保 `api_key` 与 `api_key_cmd` 永不同时存在，换 provider 时只清理上一次 senv 写入的 `senv-*` 条目。
3. **新增 `AgentAdapter.ResolveProtocol` 可选钩子支持多协议直通**。既有 7 个单协议族 agent 保持 `nil`、行为零变化；`ocrAdapter` 按档案 `api_shape` 映射目标协议族与字段：
   - `anthropic` → `ProtocolAnthropic`（取 `anthropic_base_url` 或剥离 `/v1` 的 `BaseURL`，`protocol: "anthropic"`）；
   - `openai-responses` → `ProtocolOpenAICompatible`（取 `responses_base_url` 或 `BaseURL`，`protocol: "openai-responses"`）；
   - 未声明或 `openai-chat` → `ProtocolOpenAICompatible`（取 `chat_base_url` 或 `BaseURL`，`protocol: "openai"`）。
4. **ADR-0031 凭据模式：非 Windows 默认用 `api_key_cmd: "printenv <VAR>"`，Windows 回落 `inline`**。`ocr` 的 `api_key` 不展开环境变量，但 `api_key_cmd` 原生支持外部命令取值；选用 `printenv <VAR>` 并在 `Apply` 中用 `validEnvVarName` 校验变量名，既实现默认明文不落盘，又避免 shell 引号或 `$VAR` 注入。Windows 下 `cmd.exe` 无 `printenv`，`CredentialEnvRef` 置 `nil` 使 `auto` 回落 `inline`。显式 `--credential-mode=inline` 写 `api_key` 明文（0600）。
5. **TUI 与 MCP 零代码改动；不扩展 MCP / skill 导出**。`internal/tui/ai_tab.go` 与 `cmd/mcp_llm.go` 均由 `SupportedAgents()` 驱动；`ocr` 本身是代码审查 CLI，不是 MCP/skill 宿主。

## Considered Options

- **写入内置 `providers.<name>`**：`providers` 只供内置预设表覆盖使用，自定义网关别名在 `resolver.go` 中会被忽略。
- **写入遗留 `llm` 块**：当顶层 `provider` 非空时会被 `provider` 路径遮蔽，且不支持多模型列表 `models[]` 与 `ocr config model` 切换。
- **默认一律写 `inline` 明文**：违背 ADR-0031「支持外部反解时优先不落明文」的原则；`api_key_cmd` 是上游一等支持的机制。
- **把 `ocr` 锁定为单一协议族**：会导致 `anthropic` 或 `openai-responses` 档案无法切换给 `ocr`，与上游原生能力不符。

## Consequences

- `AgentAdapter` 增加可选 `ResolveProtocol` 钩子，对既有适配器无破坏性。
- 上游漂移锚点：`alibaba/open-code-review` `v1.13.0`（`cmd/opencodereview/config_cmd.go`、`internal/llm/resolver.go`、`internal/llm/protocol.go`、`internal/llm/keycmd.go`）。

## Status

accepted（2026-11-24）
