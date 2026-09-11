# 设计：MCP remote 传输（http/sse）支持

## 决策记录（本 change）

| # | 决策 | 结论 | 理由 |
|---|------|------|------|
| D1 | 范围 | 全链路：档案 CRUD + import + 导出/撤回 + TUI/MCP 工具面 | 只入库不导出则 remote 档案是死数据；D11 的顾虑以「能力矩阵 + 只写文档化键 + 不支持即报错」正面解决 |
| D2 | 导入形态 | 扩展 `add` + 新增 `senv mcp import <file>` 批量导入 | 用户 headline 即「配置导入」；agent/脚本逐条 `add` 无法一次性吃下既有配置 |
| D3 | remote 字段 | `url`（必填）+ `headers`（可选），无 `env` | remote 条目在所有 agent 格式中均无进程环境概念；env 留给 stdio |
| D4 | 不支持的目标 | 计划标 `error` 并说明原因，绝不静默跳过 | 沿用「宁可拒绝不静默坏配置」原则（D11 反面） |

## 模块划分

- `internal/storage/mcp_server.go`：`MCPServerEntry` 增加 `URL string`、`Headers map[string]string`（均 `omitempty`）；`ValidateMCPServer` 按传输分支校验。已登记的 `mcp_servers/` 目录不变，rekey/consistency/repair 无需新增注册点。
- `internal/agentcfg`：`Server` 写入形状增加 `Transport`/`URL`/`Headers`；`serverFromMap` 解析 `type`/`url`/`headers`（读侧兼容，供指纹与 import）；新增 `TOMLServers` 枚举原语；JSON/TOML 渲染函数按传输分支。
- `internal/mcp`：`resolveEntry` 按传输解析模板（remote 解析 `url` + headers 值，无 env）；导出计划对「目标不支持该传输」产出 `error` 条目。
- `cmd/mcp_server.go`：`add`/`edit` 增 `--transport`/`--url`/`--header`/`--unset-header`；`list`/`get` 输出适配。
- `cmd/mcp_import.go`（新）：`senv mcp import <file> [--dry-run]`。
- `internal/tui/mcp_tab.go`：表单传输选择、url 字段、headers 经 `$EDITOR`；列表/详情脱敏。
- `cmd/mcp_mcp.go`：`mcpServerView` 不变（alias/transport/command/description；remote 条目 command 为空，url/headers 不出值面）。

## 数据模型

```go
type MCPServerEntry struct {
    Alias       string            // 不变
    Transport   string            // "stdio" | "http" | "sse"
    Command     string            // 仅 stdio
    Args        []string          // 仅 stdio
    Env         map[string]string // 仅 stdio；模板原样
    URL         string            // 仅 remote；模板原样
    Headers     map[string]string // 仅 remote；值模板原样
    Description string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

校验规则（保存与加载都跑）：传输 ∈ {stdio, http, sse}；stdio 要求 `command` 非空且 `url`/`headers` 为空；remote 要求 `url` 以 `http://`/`https://` 开头（允许内嵌 `{{...}}`，只需校验前缀与非法字符）且 `command`/`args`/`env` 为空；header 键 MUST 为合法 HTTP token，值无 NUL。

跨 agent 写入形状：`agentcfg.Server{Transport, Command, Args, URL, Headers, Env}`。**指纹向后兼容**：规范化序列化对零值 `Transport`/`URL`/`Headers` 省略字段——旧 stdio 台账指纹在升级后保持不变，不会把全部既有导出误判为漂移。

## 7-agent remote 能力矩阵

调研基线（2026-09，本地实机取证 + 官方文档）：

| agent | 格式与位置 | stdio | http | sse | 依据/置信 |
|---|---|---|---|---|---|
| claude-code | `~/.claude.json` `mcpServers` | ✓ | `{type:"http", url, headers}` | `{type:"sse", url, headers}` | 官方文档 + 本机实条目，高 |
| claude-desktop | `claude_desktop_config.json` | ✓ | ✗ 配置文件仅 stdio，remote 走应用内 Connectors（非文件） | ✗ | 官方/社区一致，高 |
| cursor | `~/.cursor/mcp.json` | ✓ | `{type, url, headers}`（SSE 已被 streamable 取代，同形态） | 同左（形态一致） | 本机实条目，高 |
| codex | `~/.codex/config.toml` `[mcp_servers.<alias>]` | ✓ | `url = "..."`（streamable HTTP）；headers 键名待核验 | 未文档化 → 暂按不支持 | 官方 config reference，中 |
| zcode | 实机 `~/.zcode/cli/config.json` 键 `mcp.servers`：`{type:"http", url, headers}` | ✓ | ✓ | 按 `type` 接受 | 本机实条目，高 |
| kimi | 新版 `~/.kimi-code/mcp.json`（注册表现指 `~/.kimi/mcp.json`） | ✓ | `{url, headers}` | 支持（legacy） | 官方文档，中 |
| pi | MCP 非内置，经扩展适配器读 `mcpServers` JSON | 经适配器 | `{url}`（+headers） | 经适配器 | 低-中，实现期核验 |

**实现期必须逐 agent 核验**（tasks 有专项任务）：路径/键名以当时官方文档为准；只写核验通过的文档化键；任一 (agent, transport) 组合未核验通过 → 声明为不支持，导出计划报 `error`。矩阵之外不发明键名。

**发现的注册表偏差（随本 change 修正或单列任务）**：zcode 注册表指 `~/.zcode/config.json`+`mcpServers`，实机为 `~/.zcode/cli/config.json`+`mcp.servers`；kimi 文档已迁 `~/.kimi-code/mcp.json`。此偏差同样影响既有 stdio 导出，核验后以最小改动修正 `agentcfg` 注册表。

## 数据流

```
senv mcp add --transport http --url ... --header ...
senv mcp import config.json        ──▶ vault(mcp_servers/<alias>.enc)
                                            │
senv mcp export --all ──────────────────────┤
        │                                   │
        ▼                                   ▼
  1) 读档案 ──▶ 2) 按传输解引用模板（stdio: env；remote: url+headers 值，严格模式）
        ▼
  3) 查 (agent, transport) 能力矩阵 ── 不支持 ─▶ error 条目（说明原因，文件不动）
        │ 支持
        ▼
  4) 按 agent 渲染（JSON: type/url/headers；TOML: url 表）──▶ 既有 plan→confirm→write+ledger 流程
```

`unexport` 按台账指纹删除条目，传输无关，仅 `serverFromMap` 需识别 remote 键以正确比对指纹。

## 关键取舍

- 指纹规范化对新增字段 omitempty，升级不触发全量漂移（向后兼容红线）。
- import 分类规则：显式 `type` 优先；无 `type` 有 `url` → `http`；有 `command` → `stdio`；两者皆无或 `type` 不在支持集合 → 逐条失败继续。
- import 永不覆盖：别名已存在 → 冲突跳过（与 `Add`/`ErrExists` 一致）。
- TUI/CLI 列表脱敏：remote 显示 `scheme://host` 来源，不显示 query 与 headers；`get` 与表单 `$EDITOR` 仍是唯一解密面（沿用 ADR-0008 与 TUI 值可见性语义）。
- remote 档案不支持 env：字段组合校验拒绝，避免导出时无处安放。

## 错误处理策略

| 场景 | 行为 |
|------|------|
| remote 缺 url / url 非 http(s) / 携带 stdio 字段 | 校验失败，不落库 |
| 目标 agent 不支持该传输 | 计划 `error` 条目 + 原因，该文件不动，其余 agent 继续 |
| url/header 模板解析失败 | 该 agent 报错终止（既有严格模式语义） |
| import 文件解析失败 | 整体报错，不落任何档案 |
| import 条目无法识别 / 别名冲突 | 逐条报告（失败/冲突跳过），其余继续 |
| 指纹 | remote 字段纳入指纹；旧 stdio 台账记录保持有效 |

## CLI 示例

```bash
senv mcp add web-search-prime --transport http \
  --url "https://api.example.com/mcp?key={{env:secrets:KEY}}" \
  --header "Authorization: Bearer {{env:secrets:T}}" --description "web search"
senv mcp import ~/.zcode/cli/config.json --dry-run
senv mcp import ~/.claude.json
senv mcp edit legacy --transport sse --url "https://api.example.com/sse"
senv mcp list          # remote 行显示 scheme://host
senv mcp export --all  # claude-desktop 上的 remote 条目 → error 并说明
```

## 测试策略

- storage：三传输校验矩阵（合法/字段越界/模板 url）；新旧条目互读回归。
- 指纹：新增字段 omitempty 后旧 stdio 指纹不变的回归测试（升级不漂移）。
- agentcfg：JSON/TOML 两族 remote 渲染保真（其它键保留）、读侧 `serverFromMap` 识别 `type`/`url`/`headers`、TOML 枚举。
- 导出：remote 到各支持 agent 的端到端（JSON/TOML）、claude-desktop error 条目、url/header 模板解析失败隔离、指纹漂移三态、unexport 对 remote 条目的删除。
- import：JSON/Codex TOML 夹具、分类规则四分支、冲突跳过、部分失败汇总、`--dry-run` 不落库。
- 安全：`list`/TUI 不泄漏 query/header 值；`mcp_server_list` 响应无 url/headers 值；`--print`/`--dry-run` 不落盘。
