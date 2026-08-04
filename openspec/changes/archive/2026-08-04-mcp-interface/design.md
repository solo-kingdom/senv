## 概述

`senv mcp` 子命令组把 senv 的 env/text/config 能力以 MCP tools 形式暴露给本地 AI agent，并提供一键安装到主流 agent 的能力。

## 数据流

```
agent 进程
  │  启动子进程: senv mcp serve
  ▼
senv mcp serve (stdio MCP server)
  │  1. resolveAuth(configPath, dataPath) → 命中 session cache → 拿到 derived key
  │     （无 session → 报 ErrNeedSession 退出，绝不 prompt）
  │  2. 用 key 构造 env/text/config 三个 manager
  │  3. mcp.NewServer → registerMCPTools(每个 handler 闭包持有 managers)
  │
  │  JSON-RPC over stdin/stdout（go-sdk StdioTransport）
  ▼
tools/call → handler → manager.Get/Set/... → 加密存储
                │
                └─ 解引用走 resolveValueWith(value, loose, group, envMgr, textMgr)
                   （直接用注入的 manager，不再二次 resolveAuth）
```

## 鉴权策略

- MCP 子进程的 stdin 是 JSON-RPC 传输通道，**不是 TTY**，无法弹密码框。
- server 启动期调用 `resolveAuth`：命中 session cache 则成功；否则返回 `ErrNeedSession`，server **退出**并提示 `senv session start`。
- 与 `env export --if-session` 一致的"非交互禁止 prompt"原则，避免在 agent 启动链路里 hang。

## 工具集（16 个）

| 命名空间 | 工具 |
|---|---|
| env | `senv_env_get` / `senv_env_set` / `senv_env_delete` / `senv_env_list` / `senv_env_export` |
| text | `senv_text_get` / `senv_text_set` / `senv_text_delete` / `senv_text_list` |
| config | `senv_config_list` / `senv_config_get` / `senv_config_export` |
| group | `senv_group_list` / `senv_group_add` / `senv_group_activate` / `senv_group_deactivate` |

- 键支持 `group:key` 简写地址（复用 `resolveAddressKey`）。
- `get` 支持 `decode=true` 解引用 `{{env:...}}` / `{{text:...}}`。
- 结果统一为 JSON 文本块（`mcp.TextContent`）；错误用 `IsError=true` 的 tool result，便于模型自纠。

## 安装命令（install）

- 注册表 `supportedAgents()`：claude-code / claude-desktop / cursor / codex / zcode / kimi / pi。
- 两种配置格式：
  - **JSON**（除 codex 外）：读旧配置 → upsert `mcpServers.senv` → 保留其它键 → 备份 → 写回。
  - **TOML**（codex）：按顶层 table header 切分，替换/追加 `[mcp_servers.senv]`，保留其它 table 与自由文本。
- `--print`：只输出可粘贴片段（command 保持 `senv` 可读，不解析绝对路径）。
- `--scope project`：仅 cursor 支持（写到 `.cursor/mcp.json`），其它忽略。
- `--all`：遍历注册表，逐个安装，单点失败不中断其余。

## 错误处理策略

| 场景 | 行为 |
|---|---|
| 无 session 启动 server | 退出，stderr 提示 `senv session start` |
| 工具调用业务错误（如 key 不存在） | `IsError=true` 的 tool result，内容为错误文案 |
| 配置文件已存在但解析失败 | 返回错误，不覆盖（避免破坏用户配置） |
| 安装目标已含 senv 条目 | 原地替换（不重复） |
| 安装目标含其它 server | 保留 |

## 向后兼容

- 纯新增命令，不改动既有 CLI 行为。
- `resolveValue` 保留原签名（内部改为委托 `resolveValueWith`），所有 CLI 调用方零改动。
- go.mod 最低版本不变（go 1.24.9）；go-sdk 选 v1.3.1 以满足该约束。
