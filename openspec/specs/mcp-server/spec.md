# mcp-server Specification

## Purpose
用 vault 加密保管第三方 MCP 服务器的接入定义，作为导出到各 Coding Agent 的唯一事实源。
## Requirements

### Requirement: 档案传输与字段校验

`senv mcp add <alias>` SHALL 创建一条 MCP Server 档案。档案 MUST 以别名唯一标识；传输 MUST 为 `stdio` / `http` / `sse` 之一。`stdio` 传输 `command` MUST 必填，`args` 与 `env` MAY 省略；`http` / `sse` 传输 `url` MUST 必填且 MUST 以 `http://` 或 `https://` 开头（值可含引用模板），`headers` MAY 省略，且 `command` / `args` / `env` MUST NOT 出现。别名已存在时 SHALL 报错且不修改现有档案。

#### Scenario: 创建 stdio 档案

- **WHEN** 执行 `senv mcp add github --command npx --args "-y,@modelcontextprotocol/server-github" --env GITHUB_TOKEN={{env:secrets:GH_TOKEN}}`
- **THEN** 创建成功，档案传输类型为 `stdio`，`env` 值按模板原样存储（不做引用解析）

#### Scenario: 别名冲突拒绝

- **WHEN** 对已存在的别名执行 `senv mcp add`
- **THEN** 报错说明别名已存在，现有档案保持不变

#### Scenario: 创建 http 档案

- **WHEN** 执行 `senv mcp add web-reader --transport http --url "https://api.example.com/mcp?key={{env:secrets:KEY}}" --header "Authorization: Bearer {{env:secrets:TOKEN}}"`
- **THEN** 创建成功，传输类型为 `http`，`url` 与 header 值按模板原样存储

#### Scenario: 创建 sse 档案

- **WHEN** 执行 `senv mcp add legacy --transport sse --url "https://api.example.com/sse"`
- **THEN** 创建成功，传输类型为 `sse`

#### Scenario: remote 档案携带 stdio 字段拒绝

- **WHEN** 执行 `senv mcp add web --transport http --url "https://api.example.com/mcp" --command npx`
- **THEN** 报错说明 `http` 传输不接受 `command`，不创建档案

#### Scenario: remote 缺少 url 拒绝

- **WHEN** 执行 `senv mcp add web --transport http`
- **THEN** 报错说明 `url` 必填，不创建档案

#### Scenario: url 非法拒绝

- **WHEN** 执行 `senv mcp add web --transport http --url "ftp://api.example.com/mcp"`
- **THEN** 报错说明 `url` 必须为 http(s) 地址，不创建档案

#### Scenario: 缺少 command 拒绝

- **WHEN** 传输为 `stdio` 且未提供 `--command`
- **THEN** 报错并指明 `command` 必填

### Requirement: 档案查看与编辑

`senv mcp get <alias>` SHALL 输出该档案的完整字段（含 `env`、`url`、`headers` 值）。`senv mcp edit <alias>` SHALL 允许更新传输类型与对应传输字段（remote 增加 `--url`、`--header`、`--unset-header`），且 MUST NOT 改变别名。编辑结果的字段组合 MUST 满足目标传输的校验规则，违反时 SHALL 报错且不修改现有档案。别名不存在时两者 SHALL 报错。

#### Scenario: 查看完整档案

- **WHEN** 执行 `senv mcp get github`
- **THEN** 输出命令、参数、环境变量等完整字段

#### Scenario: 查看 remote 档案

- **WHEN** 执行 `senv mcp get web-reader`
- **THEN** 输出传输类型、完整 `url` 与全部 header 键值

#### Scenario: 编辑字段

- **WHEN** 执行 `senv mcp edit github --args "-y,@modelcontextprotocol/server-github@latest"`
- **THEN** 仅该字段被更新，别名与其它字段不变

#### Scenario: 编辑 remote 字段

- **WHEN** 执行 `senv mcp edit web-reader --url "https://api.example.com/v2/mcp" --header "X-Api-Key: {{env:secrets:KEY}}"`
- **THEN** `url` 被替换，headers 整体被替换为给定集合，别名与传输类型不变

#### Scenario: 传输切换校验失败回滚

- **WHEN** 对 stdio 档案执行 `senv mcp edit github --transport http`（未提供 `--url`）
- **THEN** 报错说明 `http` 传输需要 `url`，档案保持 stdio 且字段不变

### Requirement: 档案列举与删除

`senv mcp list` SHALL 按别名稳定排序列出档案摘要（别名、传输类型、命令概要；remote 条目显示 `url` 的 `scheme://host` 来源），且 MUST NOT 输出 `env` 值、header 名/值或 `url` 的 query。`senv mcp delete <alias>` SHALL 删除档案，且 MUST NOT 改动任何 agent 配置文件。

#### Scenario: 列举不泄漏值

- **WHEN** 存在含 `env` 值、headers 与带 query 的 `url` 的档案时执行 `senv mcp list`
- **THEN** 输出不含任何 `env` 值、header 名/值与 query，仅有元信息摘要

#### Scenario: remote 条目显示来源

- **WHEN** 存在档案 `web-reader`（`url` 为 `https://api.example.com/mcp?key=secret`）时执行 `senv mcp list`
- **THEN** 该行显示传输 `http` 与来源 `https://api.example.com`，不出现 `key=secret`

#### Scenario: 删除不触碰 agent 配置

- **WHEN** 执行 `senv mcp delete github`
- **THEN** vault 中档案被删除，agent 配置文件字节不变，并提示可用 `senv mcp unexport` 清理已导出的条目

### Requirement: 值引用模板

档案的 `env` 值、remote 档案的 `url` 与 header 值 SHALL 支持 `{{env:<group>:<key>}}` 与 `{{text:<group>:<key>}}` 引用模板，存储时 MUST 保存原样模板、不做解析。

#### Scenario: 存储原样模板

- **WHEN** 创建档案时 `env` 值为 `{{env:secrets:GH_TOKEN}}`
- **THEN** vault 中保存该字面模板，导出时才解析

#### Scenario: url 与 header 模板原样存储

- **WHEN** 创建档案时 `url` 含 `{{env:secrets:KEY}}`、header 值为 `Bearer {{text:secrets:T}}`
- **THEN** vault 中保存该字面模板，导出时才解析
