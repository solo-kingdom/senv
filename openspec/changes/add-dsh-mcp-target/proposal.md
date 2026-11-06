# DSH（DeepSeek Harness）MCP 导出/安装目标

## Why

DSH（本机 Desktop 0.2.2-rc.1）的 MCP server 不是 `mcpServers` 对象，而是 cordis loader 的 patch 行：只有 `- insert: [...]` 才会新增 `@deepseek-ai/dsh-mcp-client` 实例，裸 `- id/name/config` 行只覆盖已有条目、找不到时 stderr warning + 退出码 0（静默丢弃）。senv 目前没有 `dsh` 目标，`senv mcp export/install` 管不到 DSH，「senv 是唯一事实源」在 DSH 上断掉，MCP 只能手工配。

## What Changes

- `internal/agentcfg` 新增 `FormatCordisPatch` 与 `dsh` 目标（别名 `deepseek-harness` 仅查找接受）：目标文件 `$DSH_HOME/cordis.patch.yml`（`DSH_HOME` 空则 `~/.dsh`，支持 `~` 展开），user/project 同路径；remote 只接受 stdio + streamable-http，sse plan-error；无前置依赖。
- 新增 `cordis_patch.go` 写回原语：只重写文件尾部标记块（`# >>> senv-managed MCP servers ...`），块外字节逐字节保留；所有受管 server 共用一个 `- insert:` 行（每个 server 是其中一个条目），确定性 id `senv-mcp-<serverName>`；`serverName` 不合 `^[A-Za-z0-9_-]{1,32}$` 时确定性归一 + 6 位 alias hash；删空后写 `[]` 保持层合法；写前校验顶层数组/映射、重复 id 与 serverName fail closed。
- `internal/mcp` 的 `agentFile` 增加 cordis 分支（export/unexport/drift 全走既有语义）；`cmd/mcp_install.go` 增加 DSH 安装；`cmd/mcp_export.go --print` 输出 DSH 片段。TUI 由注册表驱动自动出现。
- 文档：ADR-0034、`.agents/skills/senv-cli/SKILL.md`、`README.md` 的 agent 枚举同步。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `mcp-server-export`：导出/安装目标加入 `dsh`；新增 cordis patch 的写入形状（insert 行 + 受管标记块 + 确定性 id/serverName）、DSH 层选择（home patch）、无 sse、空层 `[]` 与冲突 fail-closed 要求。

## Non-goals

- 不扩展 `senv ai switch dsh`：DSH 的 LLM provider 是 `llm-*` bundle 的 config 重写，字段面与现有适配器不同，另案处理。
- 不写 profile 级 `profiles/<name>/cordis.patch.yml`：见 design 决策 2（全局语义、确定性路径、profile 恢复不回滚）。
- 不做 config-only bundle / 不调用 `dsh plugin`：不引入 pnpm 与 profile 清单副作用。
- 不支持 sse 档案导出到 DSH（schema 无该类型）。
- 不接管 DSH 用户自建的 mcp-client 条目：只增删改标记块内的 `senv-mcp-*` 行。

## Impact

- 代码：`internal/agentcfg/agents.go`（格式枚举 + dsh 目标）、`internal/agentcfg/cordis_patch.go`（新）、`internal/mcp/export.go`、`cmd/mcp_install.go`、`cmd/mcp_export.go`。
- 测试：`internal/agentcfg/{config_test,cordis_patch_test}.go`、`internal/mcp/export_test.go`、`cmd/{mcp_export_test,mcp_install_test}.go`、`internal/tui/mcp_tab_test.go`；所有涉及 `--all`/dsh 的测试钉住 `DSH_HOME`。
- 文档：`docs/adr/0034-dsh-mcp-target-cordis-patch.md`、`.agents/skills/senv-cli/SKILL.md`、`README.md`。
- 安全：写盘面沿用既有明文导出取舍（ADR-0008，0600 + `.bak`，计划标 `[明文]`）；不新增代码执行面，不调用 DSH 二进制。
