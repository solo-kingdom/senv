# mcp-server-export Delta

## ADDED Requirements

### Requirement: dsh 导出与安装目标

DSH（DeepSeek Harness，id `dsh`，别名 `deepseek-harness` 仅查找接受）的 MCP 配置是 cordis loader patch 行，MUST NOT 有前置依赖（`@deepseek-ai/dsh-mcp-client` 随 DSH 安装包提供）。目标文件 MUST 为 home 级用户 patch `<DSH_HOME>/cordis.patch.yml`：`DSH_HOME` 非空（非纯空白）时使用并按 DSH 规则展开 `~`，否则为 `<home>/.dsh/cordis.patch.yml`；`user` 与 `project` scope MUST 解析到同一文件（DSH 无项目级 patch 层）。写入 MUST 只修改文件尾部的 senv 受管标记块（`# >>> senv-managed MCP servers ...`），块外字节（注释、`!!js` 表达式、用户条目、条目顺序）MUST 逐字节保留。

每个 MCP 档案 MUST 落成受管 `- insert:` patch 行里的一个条目（同一受管块共用一个 patch 行；patch 层新增条目只有 insert 形状可行），条目 id 固定为 `senv-mcp-<serverName>`；MUST NOT 写裸 `- id/name/config` 行（DSH 对不存在的 id 只输出 stderr warning、退出码 0，条目被静默丢弃）。`serverName` MUST 满足 `^[A-Za-z0-9_-]{1,32}$`：合规别名原样使用，不合规别名 MUST 确定性归一（非法字节换 `-`、截断到 32 字节预算）并追加 alias 的短 hash 后缀，使不同别名不会塌缩到同一 namespace。stdio 档案写 `transport: stdio` 与 `command/args/env`；http 档案写 `transport: streamable-http` 与 `url/headers`。写盘后无需重启：DSH 的 `dsh-hmr` watch home patch 并热重载；仅在 HMR 被禁用时提示重启。

#### Scenario: 安装 senv server 到 dsh

- **WHEN** 用户执行 `senv mcp install dsh`
- **THEN** `<DSH_HOME>/cordis.patch.yml` 的受管块内出现 `senv-mcp-senv` 的 `insert` 行（stdio、命令为 senv 可执行文件），输出不出现前置依赖提示

#### Scenario: 导出 stdio 档案

- **WHEN** 导出 `stdio` 档案 `github` 到 dsh
- **THEN** 受管块写入 `- insert:` 行，`id: senv-mcp-github`、`name: '@deepseek-ai/dsh-mcp-client'`、`serverName: github`、`transport: stdio` 与 `command/args/env`，块外内容不变

#### Scenario: 导出 http 档案

- **WHEN** 导出 `http` 档案 `web` 到 dsh
- **THEN** 条目写 `transport: streamable-http`、`url` 与 `headers`，不出现 `command`

#### Scenario: 幂等导出

- **WHEN** 同一档案连续导出两次到 dsh
- **THEN** 第二次计划为 `skip`，文件字节与第一次完全一致

#### Scenario: 用户自建条目保留

- **WHEN** `cordis.patch.yml` 已含用户手写的 id 覆盖行、`!!js` 表达式或其它 mcp-client 条目
- **THEN** 导出后这些内容逐字节保留，senv 不改写也不接管；senv 只在受管块内增删自己的条目

#### Scenario: 本地修改判 drift

- **WHEN** 用户手工修改受管块内某条目的 `command`/`url`/`env`
- **THEN** 计划把该条目判为 `drift`，默认不覆盖；`--force` 才恢复为档案定义

#### Scenario: 撤回后保持层合法

- **WHEN** 用户执行 `senv mcp unexport --agent dsh` 移除最后一个受管条目
- **THEN** 受管块被整体删除；若文件其余内容为空/仅注释，写入显式 `[]`（DSH 对空 patch 文件启动报错），文件仍为合法 patch 列表

#### Scenario: DSH_HOME 覆盖

- **WHEN** `DSH_HOME=/alt/dsh` 已设置，用户导出到 dsh 的 user scope
- **THEN** 目标路径为 `/alt/dsh/cordis.patch.yml`

#### Scenario: serverName 归一

- **WHEN** 档案别名含 `.`、空格或超过 32 字符（如 `my.server`）
- **THEN** 写入的 `serverName` 匹配 `^[A-Za-z0-9_-]{1,32}$`、由别名确定且不同别名不冲突；`id` 以 `senv-mcp-` 开头并同样确定

#### Scenario: 别名等价

- **WHEN** 用户执行 `senv mcp export --agent deepseek-harness`
- **THEN** 计划与写入结果与 `--agent dsh` 完全一致

### Requirement: dsh 拒绝 sse 传输

`@deepseek-ai/dsh-mcp-client` 的 transport 只有 `stdio` 与 `streamable-http`，schema 无 `sse` 值。导出 `sse` 档案到 dsh MUST 在计划阶段标 `error` 并说明原因，MUST NOT 写任何文件、MUST NOT 把它静默改写为 `streamable-http`。

#### Scenario: sse 档案 plan-error

- **WHEN** 导出 `sse` 传输的档案到 dsh
- **THEN** 计划中该条目标注 `error` 并说明 DSH 只接受 stdio 与 streamable-http，`cordis.patch.yml` 字节不变

### Requirement: dsh 写盘冲突校验

写盘前 senv MUST 把最终文件解析为 DSH patch 列表（顶层数组、每项映射）并校验：每个受管 id 与 `serverName` 在文件中恰好出现一次。校验失败（受管块损坏、块内夹带非 senv 条目、块外出现 senv id 前缀（无论 insert 条目还是 id-targeted 覆盖行）、两个别名归一后落到同一 id、`serverName` 与文件内其它条目重复）MUST 拒绝写入并报错，MUST NOT 留下部分写入或重复条目。追加受管块前 MUST 只替换解析到的顶层空列表节点，MUST NOT 删除多行标量或嵌套序列里的 `[]` 文本行。

#### Scenario: 重复 serverName 拒绝写入

- **WHEN** 文件中已有用户自建 mcp-client 条目使用与待导出档案相同的 `serverName`
- **THEN** 导出以错误结束，文件字节不变

#### Scenario: 受管块损坏 fail closed

- **WHEN** 受管标记块缺少结束标记、块内出现非 mcp-client 条目或裸 id 行
- **THEN** 计划该目标标 `error` 并给出修复指引，不覆盖文件
