## Why

senv 的全部能力都封装在 CLI 里，本地 AI agent（Claude Code/Desktop、Cursor、Codex、ZCode、Kimi、PI 等）无法在不复制粘贴明文的情况下读取/管理秘密。需要为 agent 提供一个标准的本地接入点（MCP），并把"把 senv 装进 agent"这件事封装成一条命令，免去手工编辑各家配置文件。

## What Changes

- 新增 `senv mcp serve`：以 **stdio MCP server** 形式运行（基于官方 `github.com/modelcontextprotocol/go-sdk`），把 env/text/config/group 能力暴露为 16 个工具。
- 新增 `senv mcp install <agent>`：把 `{command: senv, args: [mcp, serve]}` 写入目标 agent 配置；**保留既有配置与其它 MCP server**，写前生成 `.bak`；支持 `--scope user|project`、`--print`、`--all`。
- 新增 `senv mcp list-tools`：打印工具清单（兼作冒烟自检）。
- 鉴权模型：server 启动即复用 `resolveAuth`（命中 session cache）；无有效 session 时**报错退出并提示 `senv session start`**，**绝不**在非 TTY 的 MCP 子进程里弹密码（与 `env export` 的 `ErrNeedSession` 同策略）。
- 引擎层：抽出 `resolveValueWith(value, loose, group, envMgr, textMgr)`，让 server 在启动期一次性鉴权后把 manager 注入 handler，解引用不再二次鉴权。

## Capabilities

### New Capabilities

- `mcp-interface`：本地 stdio MCP server（仅 tools）；启动期 session 鉴权一次，进程内缓存密钥；提供安装到主流 agent 的命令。

### Modified Capabilities

- （无行为变更；`ref-system` 的解引用逻辑通过新入口 `resolveValueWith` 复用，CLI 路径行为不变。）

## Impact

- 代码：`cmd/mcp.go`、`cmd/mcp_agents.go`、`cmd/mcp_install.go`、`cmd/mcp_test.go`、`cmd/mcp_install_test.go`；`cmd/text.go`（抽出 `resolveValueWith`）；`go.mod`/`go.sum`（新增 go-sdk v1.3.1）；`README.md`。
- 安全：MCP server 把 env/text/config 的读写能力交给目标 agent 上下文中的模型；由用户主动 `install` 触发，配置写入前备份；敏感值仅作为 tool result 文本返回，不进结构化元数据。鉴权复用既有 session，无新攻击面。
- 兼容：纯新增命令；不改动 CLI 既有行为。go 最低版本维持 1.24.9（go-sdk 选 v1.3.1 以兼容；v1.4+ 要求 go 1.25）。

## Non-goals

- 不做远程/网络 MCP（无 HTTP/SSE server、不监听端口）。
- 不实现 MCP 的 resources/prompts 能力，仅 tools。
- 不在 MCP 层引入新的鉴权机制（完全复用 session）。
- 不自动安装/升级 agent 自身。
