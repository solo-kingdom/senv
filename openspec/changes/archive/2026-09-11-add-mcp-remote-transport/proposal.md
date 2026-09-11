## Why

MCP Server 档案 V1 仅支持 stdio（`add-mcp-server-export` D11 决策），含 HTTP remote（`http`/`sse`）server 的配置无法导入 senv：`senv mcp add` 无 URL 形态，批量导入时 remote 条目全部失败。remote MCP 已是主流接入方式，档案能力缺一半。

## What Changes

- 档案传输类型扩展为 `stdio` / `http` / `sse`：新增 `url`（必填于 remote，支持 `{{env:...}}`/`{{text:...}}` 模板）与 `headers` 字段；stdio 语义不变。
- CLI：`senv mcp add`/`edit` 新增 `--transport`/`--url`/`--header`；`get`/`list` 按 remote 字段输出（list 仍不泄漏值）。
- 新增 `senv mcp import <file>`：批量导入既有 agent 配置文件（JSON `mcpServers` 族与 Codex TOML），stdio/remote 条目一并建档，别名冲突默认跳过。
- 导出/撤回：按各 agent 的 remote 键名矩阵写入/移除 remote 条目；agent 配置格式无法表达某传输时报错而非静默跳过；url/headers 纳入明文落盘标注与指纹。
- TUI MCP Tab 与 `mcp_server_list` 工具面适配 remote 字段（值面沿用既有脱敏规则）。

## Capabilities

### New Capabilities

- `mcp-server-import`：`senv mcp import` 批量导入 agent 配置文件为档案（识别传输类型、冲突跳过、逐条报告）。

### Modified Capabilities

- `mcp-server`：档案创建/查看/编辑/列举接受 `http`/`sse` 传输与 `url`/`headers` 字段；值引用模板覆盖 url 与 header 值。
- `mcp-server-export`：格式映射与合并扩展 remote 形态；无法表达该传输的目标报错；明文落盘提示覆盖 url/headers。
- `mcp-server-tui`：新建/编辑表单支持传输选择与 url/headers；值可见性规则扩展到 url 与 headers。

## Impact

- `internal/storage/mcp_server.go`（entry 加字段与校验）、`internal/mcp/`（manager/exporter）、`internal/agentcfg/`（Server 形状、remote 渲染与解析、TOML 枚举）、`cmd/mcp_server.go`、`cmd/mcp_export.go`、新增 `cmd/mcp_import.go`、`internal/tui/mcp_tab.go`。
- 无破坏性变更：既有 stdio 档案与校验行为不变，存储为纯增量字段。
- 同步更新 `.agents/skills/senv-cli/SKILL.md`（新增命令与 flags）。

## 非目标

- 不做连通性探测/协议握手验证，senv 只管配置不代理 MCP。
- 不做 OAuth 类动态授权，remote 仅支持静态 url 与 headers。
- 不透传 agent 特有键（沿用 D18）。
- `import` 只接受显式文件路径，不隐式直读各 agent 现役配置。
- server provider 同步清单不变（沿用 ssh/LLM 同款现状边界）。

## 安全性分析

url/headers 含敏感凭证（API key、Bearer token），静态存储沿用 AES-256-GCM 加密入库；导出时明文落盘沿用 ADR-0008 取舍，计划页显式标注。`senv mcp list` 与 TUI 列表/详情 MUST NOT 渲染 url query 与 header 值（`get` 与表单 `$EDITOR` 仍是唯一解密面）；`mcp_server_list` 继续不返回任何值字段。
