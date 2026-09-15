## MODIFIED Requirements

### Requirement: Reference resolution timing
存储时 SHALL 保存原始模板，不做任何引用解析。`env export` 与 MCP `senv_env_export` SHALL 自动解引用所有导出值中的引用；引用目标缺失时 SHALL 按「env export 宽松解引用」处理（保留模板、stderr 警告、命令 exit 0），而非整命令失败。`env get`、`env list`、`text get` SHALL 默认原样输出，仅在指定 `-d`/`--decode` 时解引用；这些命令在未指定 `--loose` 时仍遵循严格模式。

#### Scenario: env export auto-resolves
- **WHEN** 用户执行 `senv env export`，且所有引用目标均存在
- **THEN** 系统 SHALL 自动递归解引用所有导出值中的引用，stdout 输出解析后的 `export` 语句

#### Scenario: env export with missing reference
- **WHEN** 用户执行 `senv env export`，某导出值的引用目标不存在，且不存在循环引用或超深度
- **THEN** 系统 SHALL 仍 exit 0；该变量 stdout 保留未解析 `{{...}}` 模板；stderr 输出 warning 且 MUST 标注 env key 名；其余可解析变量 SHALL 正常导出

#### Scenario: env export structural error still fails
- **WHEN** 用户执行 `senv env export`，某导出值存在循环引用或超过最大递归深度
- **THEN** 系统 SHALL 以非 0 退出并报错，不输出部分解析结果

#### Scenario: env get without decode flag
- **WHEN** 用户执行 `senv env get DB_URL`（值包含 `{{text:secrets:DB_PASS}}`）
- **THEN** 系统 SHALL 原样输出包含 `{{text:secrets:DB_PASS}}` 的值

#### Scenario: env get with decode flag
- **WHEN** 用户执行 `senv env get DB_URL -d`
- **THEN** 系统 SHALL 递归解引用并输出最终值

#### Scenario: text get with decode flag
- **WHEN** 用户执行 `senv text -g configs get APP_CONFIG -d`
- **THEN** 系统 SHALL 递归解引用值中的所有 `{{...}}`

#### Scenario: text set stores raw template
- **WHEN** 用户执行 `senv text -g templates set CONF "url={{env:prod:DB_URL}}"`
- **THEN** 系统 SHALL 存储原始字符串 `url={{env:prod:DB_URL}}`，不做解析

## ADDED Requirements

### Requirement: env export per-variable resolution
`env export` SHALL 逐条 env 变量解引用后再组装 export 语句，MUST NOT 因单条失败而跳过其余条目（结构性错误除外）。warning 文本 SHALL 包含 env key 名与未解析引用，便于定位 stale 条目。

#### Scenario: Partial export with one stale ref
- **WHEN** 激活组含 `GOOD=ok` 与 `BAD={{text:llm-keys:missing}}`，且 `missing` 不存在
- **THEN** stdout 同时含 `export GOOD='ok'` 与保留模板的 `export BAD='{{text:llm-keys:missing}}'`；stderr 含针对 `BAD` 的 warning；exit 0

#### Scenario: MCP env export matches CLI
- **WHEN** agent 调用 MCP `senv_env_export`，且存在与 CLI 相同的 stale 引用
- **THEN** 行为 SHALL 与 `senv env export` 一致：返回已解析 exports 文本、warnings 经 MCP 错误通道或等效 stderr 语义暴露，且不因缺失目标而 tool 失败
