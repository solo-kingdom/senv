# mcp-server-export Delta

## ADDED Requirements

### Requirement: omp 导出与安装目标

omp（Oh My Pi，id `omp`，别名 `oh-my-pi` 仅查找接受）是内置 MCP 支持的 Coding Agent，MUST NOT 有前置依赖组件（区别于 pi 的 pi-mcp-adapter）：install 与 export 写盘前 MUST NOT 尝试安装任何扩展，输出也不要求列出前置依赖。目标路径：`user` scope 为 omp 用户级配置目录（解析规则见 llm-provider-switch「omp 配置路径解析」）下的 `mcp.json`；`project` scope 为 CWD 相对的 `.omp/mcp.json`。条目形状属 JSON 家族：stdio 写 `{command,args,env}`（`type` 键省略），remote 写 `{type: "http"|"sse", url, headers}`，`type` 键 MUST 写出（omp 对 remote 条目不能自动识别传输）。写盘后提示用户在 OMP 内 `/mcp reload` 或重启生效。

#### Scenario: 安装到 user scope

- **WHEN** 用户执行 `senv mcp install omp`（无 profile 环境变量）
- **THEN** senv 的 MCP server 写入 `~/.omp/agent/mcp.json` 的 `mcpServers`，过程中无前置依赖安装步骤，输出提示 `/mcp reload`

#### Scenario: 安装到 project scope

- **WHEN** 用户在项目目录执行 `senv mcp install omp --scope project`
- **THEN** 目标路径为该项目 `.omp/mcp.json`（CWD 相对）

#### Scenario: 导出 remote 档案写 type 键

- **WHEN** 导出 `http` 传输的 MCP Server 档案到 omp
- **THEN** omp 的 `mcp.json` 条目含 `"type": "http"`、`url` 与 `headers`；同文件的用户自有条目与其它顶层键（如 `disabledServers`）原样保留

#### Scenario: profile 激活时导出跟随

- **WHEN** `OMP_PROFILE=work` 已设置，用户导出到 omp 的 user scope
- **THEN** 目标路径为 `~/.omp/profiles/work/agent/mcp.json`
