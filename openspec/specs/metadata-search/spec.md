# metadata-search Specification

## Purpose

为 CLI 与 MCP 提供跨 vault 类型的元数据模糊搜索：按标识与说明定位条目，结果永不包含秘密值，供 agent 与脚本在条目增多时安全检索。

## Requirements

### Requirement: CLI search 命令

系统 SHALL 提供 `senv search <query>`：在已解锁 vault（有效 session）上跨 Env/Text/Backup/Config/SSH/LLM/MCP 检索。缺少 query（无参数或仅空白）时 MUST 报错且不枚举全库。可选 `--kind`（可重复或逗号分隔）限制类型；可选 `--limit`（默认 20，上限实现自定但 MUST ≥1）；可选 `--json` 输出结构化结果。人类可读默认输出每行一条命中摘要，MUST NOT 打印 value。

#### Scenario: 空 query 拒绝

- **WHEN** 执行 `senv search` 或 `senv search "   "`
- **THEN** 命令失败并提示需要 query，不输出命中列表

#### Scenario: 命中 env 元数据

- **WHEN** 存在 `svc:LITELLM_UI_PASSWORD` 且 description 含 `LiteLLM`，执行 `senv search litellm`
- **THEN** 结果含 kind=env、group=svc、key=LITELLM_UI_PASSWORD 与 description，不含 value

#### Scenario: kind 过滤

- **WHEN** 执行 `senv search web --kind ssh`
- **THEN** 仅返回 SSH 命中（若有），不含其他 kind

#### Scenario: limit 截断

- **WHEN** 匹配数超过 `--limit N`
- **THEN** 最多返回 N 条，并标明截断（如 `truncated=true` 或等价提示）

### Requirement: MCP 工具 senv_search

MCP 工具面 SHALL 提供 `senv_search`，参数至少含必填 `query`、可选 `kind`、`limit`（默认与 CLI 一致）。语义与 CLI 相同：元数据命中、无值。无有效 session 时 MUST 鉴权失败且不返回数据。`senv mcp list-tools` SHALL 列出该工具。

#### Scenario: MCP 搜索成功

- **WHEN** 有 session 且调用 `senv_search` 且 query 匹配某 text key
- **THEN** 返回含 kind/group/key（及 size/description 若适用）的命中，无 value 字段

#### Scenario: MCP 空 query

- **WHEN** 调用 `senv_search` 且 query 为空或仅空白
- **THEN** 返回错误，不返回全库枚举

#### Scenario: MCP 无 session

- **WHEN** 无有效 session 时调用 `senv_search`
- **THEN** 返回鉴权错误，不返回命中

### Requirement: 可匹配字段表

搜索 MUST 只对非秘密标识与说明字段做匹配。env/text/backup：group、key、条目 description、组 description。config：group、name、description、target path。ssh：alias、hostname、group、tags、description。llm：provider alias、description。mcp：alias、command、description。

#### Scenario: description 可命中

- **WHEN** env key 为 `X`、description 为 `payment gateway token`，query 为 `payment`
- **THEN** 该条目出现在命中中

#### Scenario: SSH hostname 可命中

- **WHEN** host alias 为 `box`、hostname 为 `web.internal`，query 为 `web.internal`
- **THEN** 该 SSH 条目出现在命中中

### Requirement: 匹配算法

query 经 trim 后按空白分词；每个 token MUST 以大小写不敏感的子串方式命中同一条目的可匹配文本（AND）。单 token 无空白时按整串匹配。

#### Scenario: 多 token AND

- **WHEN** 存在 key=`LITELLM_UI_PASSWORD`，query 为 `lite password`
- **THEN** 该条目命中；仅含 `lite` 而不含 `password` 相关可匹配文本的条目不因第二 token 而单独放宽

### Requirement: 禁止匹配与返回秘密

搜索 MUST NOT 匹配或返回 env/text/backup value、SSH/KeyPair 私钥材料、LLM 凭据明文、MCP env/header 秘密值。结果可含非秘密 preview（size、config target、ssh 连接摘要、llm 模型数、mcp command）。保留组 `llm-keys` 的 text 条目 MAY 出现在命中中（仅元数据）；本工具 MUST NOT 因此放宽对该组的 MCP get/set。

#### Scenario: 仅值中出现的串不命中

- **WHEN** 某串仅存在于某 env value 中，而不在任何可匹配字段中
- **THEN** `senv search` / `senv_search` 对该串返回空命中集

#### Scenario: 结果无 value 键

- **WHEN** 以 `--json` 或 MCP 返回命中
- **THEN** JSON 中无 `value` 字段，亦无私钥或凭据字段

### Requirement: 排序与稳定输出

命中排序 SHALL 稳定：优先更强匹配（精确 key/name > 前缀 > 子串），同档按 kind → group → key/name 字典序。无匹配时 CLI 以非错误方式给出空结果（exit 0）；MCP 返回空 hits 数组。

#### Scenario: 无匹配

- **WHEN** query 合法但不匹配任何条目
- **THEN** 返回空命中集且命令/工具成功（非鉴权/参数错误）
