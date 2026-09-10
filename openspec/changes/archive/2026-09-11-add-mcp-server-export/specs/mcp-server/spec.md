## Purpose

用 vault 加密保管第三方 MCP 服务器的接入定义，作为导出到各 Coding Agent 的唯一事实源。

## ADDED Requirements

### Requirement: 档案创建与字段校验

`senv mcp add <alias>` SHALL 创建一条 MCP Server 档案。档案 MUST 以别名唯一标识；V1 MUST 仅接受 `stdio` 传输；`command` MUST 必填，`args` 与 `env` MAY 省略。别名已存在时 SHALL 报错且不修改现有档案。

#### Scenario: 创建 stdio 档案

- **WHEN** 执行 `senv mcp add github --command npx --args "-y,@modelcontextprotocol/server-github" --env GITHUB_TOKEN={{env:secrets:GH_TOKEN}}`
- **THEN** 创建成功，档案传输类型为 `stdio`，`env` 值按模板原样存储（不做引用解析）

#### Scenario: 别名冲突拒绝

- **WHEN** 对已存在的别名执行 `senv mcp add`
- **THEN** 报错说明别名已存在，现有档案保持不变

#### Scenario: 非 stdio 传输拒绝

- **WHEN** 指定 `--transport http` 或 `--transport sse`
- **THEN** 报错说明 V1 仅支持 `stdio`，不创建档案

#### Scenario: 缺少 command 拒绝

- **WHEN** 未提供 `--command`
- **THEN** 报错并指明 `command` 必填

### Requirement: 档案查看与编辑

`senv mcp get <alias>` SHALL 输出该档案的完整字段（含值）。`senv mcp edit <alias>` SHALL 允许更新传输字段，且 MUST NOT 改变别名。别名不存在时两者 SHALL 报错。

#### Scenario: 查看完整档案

- **WHEN** 执行 `senv mcp get github`
- **THEN** 输出命令、参数、环境变量等完整字段

#### Scenario: 编辑字段

- **WHEN** 执行 `senv mcp edit github --args "-y,@modelcontextprotocol/server-github@latest"`
- **THEN** 仅该字段被更新，别名与其它字段不变

### Requirement: 档案列举与删除

`senv mcp list` SHALL 按别名稳定排序列出档案摘要（别名、传输类型、命令概要），且 MUST NOT 输出 `env` 值。`senv mcp delete <alias>` SHALL 删除档案，且 MUST NOT 改动任何 agent 配置文件。

#### Scenario: 列举不泄漏值

- **WHEN** 存在含 `env` 值的档案时执行 `senv mcp list`
- **THEN** 输出不含任何 `env` 值，仅有元信息摘要

#### Scenario: 删除不触碰 agent 配置

- **WHEN** 执行 `senv mcp delete github`
- **THEN** vault 中档案被删除，agent 配置文件字节不变，并提示可用 `senv mcp unexport` 清理已导出的条目

### Requirement: 值引用模板

档案的 `env` 值 SHALL 支持 `{{env:<group>:<key>}}` 与 `{{text:<group>:<key>}}` 引用模板，存储时 MUST 保存原样模板、不做解析。

#### Scenario: 存储原样模板

- **WHEN** 创建档案时 `env` 值为 `{{env:secrets:GH_TOKEN}}`
- **THEN** vault 中保存该字面模板，导出时才解析
