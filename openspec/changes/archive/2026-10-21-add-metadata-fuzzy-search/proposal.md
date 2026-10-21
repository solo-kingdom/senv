# Proposal：元数据模糊搜索（CLI + MCP）

## Why

vault 条目增多后，agent 只能靠各 kind 的 `list` 扫库定位；`senv_env_list` 还会把 value 拉进上下文。人侧已有 TUI 全局搜索（只匹配标识），agent 缺少对等、且强制不返回值的检索面。

## What Changes

- 新增 CLI `senv search <query>` 与 MCP `senv_search`：跨 Env/Text/Backup/Config/SSH/LLM/MCP 做元数据模糊搜索。
- 匹配字段对齐并扩展 TUI：标识 + description（及既有 SSH hostname/tags、MCP command 等）；**永不匹配、永不返回 value / 私钥 / 凭据 / MCP env 值**。
- 空 query 拒绝；默认 limit，可按 kind 过滤；CLI 支持人类可读与 `--json`。
- 抽出共享匹配/inventory 逻辑，供 CLI/MCP（及 TUI 对齐）复用。
- 更新 `senv-cli` skill 与 `mcp list-tools` 清单。

## Non-goals

- 不改 `senv_env_list` 默认带 value 的既有契约。
- 不做 server 端远程索引；不做编辑距离/fzf 式打分（v1 为大小写不敏感 substring + 多 token AND）。
- 不在 search 路径做引用 decode；不开放对 `llm-keys` 经 MCP 取值。

## Capabilities

### New Capabilities

- `metadata-search`：跨类型元数据搜索的契约（CLI、MCP、匹配字段、结果形状、安全边界、空 query/limit）。

### Modified Capabilities

- `tui-viewer`：全局搜索匹配字段与 CLI/MCP 对齐（env/text/config 等纳入 description；仍绝不匹配值）。

## Impact

- 代码：`cmd/search.go`（或等价）、MCP 注册、`internal/search`（或等价共享包）、TUI `search.go` 对齐、测试与 skill。
- 安全：只读检索面；结果与审计均不含值；需 session。

## 安全性分析

搜索是只读、元数据面：inventory 可解密条目文件以取 description/size，但结果结构禁止携带 value。MCP 工具经既有鉴权守卫；`llm-keys` 可出现在命中（group/key/description）以便定位，取值仍仅 CLI/TUI。空 query 拒绝，降低无意识全库枚举进 agent 上下文的风险（仍可用既有 list）。
