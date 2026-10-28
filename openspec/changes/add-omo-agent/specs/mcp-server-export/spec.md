# mcp-server-export Delta

## ADDED Requirements

### Requirement: omo 导出与安装目标

omo（OmO，`code-yeongyu/oh-my-openagent`，id `omo`，别名 `oh-my-openagent` 仅查找接受）是内置 MCP 支持的 Coding Agent，MUST NOT 有前置依赖组件（区别于 pi 的 pi-mcp-adapter）：install 与 export 写盘前 MUST NOT 尝试安装任何扩展，输出也不列出前置依赖。目标路径：`user` scope 为 omo agent 目录（解析规则见 llm-provider-switch「omo 配置路径解析」）下的 `mcp.json`；`project` scope 为 CWD 相对的 `.omo/mcp.json`。条目形状属 JSON 家族：stdio 写 `{command,args,env}`，remote 写 `{type: "http", url, headers}`，`type` 键 MUST 写出。写盘后提示用户在 OmO 内 `/mcp` 重新加载或重启生效。

#### Scenario: 安装到 user scope

- **WHEN** 用户执行 `senv mcp install omo`
- **THEN** senv 的 MCP server 写入 `~/.omo/agent/mcp.json` 的 `mcpServers`，过程中无前置依赖安装步骤，输出提示 `/mcp` 重新加载

#### Scenario: 安装到 project scope

- **WHEN** 用户在项目目录执行 `senv mcp install omo --scope project`
- **THEN** 目标路径为该项目 `.omo/mcp.json`（CWD 相对）

#### Scenario: 导出 remote 档案写 type 键

- **WHEN** 导出 `http` 传输的 MCP Server 档案到 omo
- **THEN** `mcp.json` 条目含 `"type": "http"`、`url` 与 `headers`

#### Scenario: 同文件用户自有条目原样保留

- **WHEN** `~/.omo/agent/mcp.json` 已含用户自建的 server 与其它顶层键，用户导出 senv 档案到 omo
- **THEN** 用户条目与顶层键原样保留，只增改 senv 管理的 `mcpServers` 条目

#### Scenario: 环境变量目录生效

- **WHEN** `OMO_CODING_AGENT_DIR=$HOME/alt` 已设置，用户导出到 omo 的 user scope
- **THEN** 目标路径为 `$HOME/alt/mcp.json`

#### Scenario: 别名等价

- **WHEN** 用户执行 `senv mcp export --agent oh-my-openagent`
- **THEN** 计划与写入结果与 `--agent omo` 完全一致

### Requirement: omo 拒绝 sse 传输

omo 的 `mcp.json` 配置 schema 把 `type` 限定为 `stdio` 与 `http`，没有 `sse` 类型（SSE 是 http 连接内部的降级路径，不是可写的类型值），且 schema 不接受未声明字段。因此导出 `sse` 传输的档案到 omo MUST 在计划阶段报错，且 MUST NOT 写任何文件、MUST NOT 改动该目标配置，MUST NOT 把它静默改写为 `type: "http"`。

#### Scenario: sse 档案 plan-error

- **WHEN** 导出 `sse` 传输的 MCP Server 档案到 omo
- **THEN** 计划中该条目标注 `error` 并说明 omo 只接受 stdio 与 http，`mcp.json` 字节不变
