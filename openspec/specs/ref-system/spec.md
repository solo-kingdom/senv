## ADDED Requirements

### Requirement: Reference syntax
系统 SHALL 支持在 env 和 text 的值中嵌入引用模板。引用格式为 `{{type:key}}` 或 `{{type:group:key}}`，其中 type MUST 为 `env` 或 `text`。不含类型前缀的 `{{...}}` SHALL 视为原文本，不做解析。`\{{` SHALL 作为转义，输出字面 `{{` 而不触发解析。

#### Scenario: Reference with explicit group
- **WHEN** 值包含 `{{text:secrets:DB_PASS}}`
- **THEN** 系统 SHALL 从 text group `secrets` 中查找 key `DB_PASS`

#### Scenario: Reference without group
- **WHEN** 值包含 `{{env:DATABASE_URL}}`，当前 `-g` 为 `prod`
- **THEN** 系统 SHALL 先在 env group `prod` 中查找，找不到则在 `default` group 中查找

#### Scenario: Literal braces via escape
- **WHEN** 值包含 `\{{env:key}}`
- **THEN** 解引用时 SHALL 输出 `{{env:key}}` 原样文本，不解析为引用

#### Scenario: No type prefix is literal
- **WHEN** 值包含 `{{not_a_ref}}`
- **THEN** 系统 SHALL 视为原文本，不做任何解析

### Requirement: Reference resolution timing
存储时 SHALL 保存原始模板，不做任何引用解析。`env export` SHALL 自动解引用所有值。`env get`、`env list`、`text get` SHALL 默认原样输出，仅在指定 `-d`/`--decode` 时解引用。

#### Scenario: env export auto-resolves
- **WHEN** 用户执行 `senv env export`
- **THEN** 系统 SHALL 自动递归解引用所有导出值中的引用

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

### Requirement: Strict mode (default)
解引用时默认 SHALL 为严格模式：引用目标不存在时 MUST 报错并终止。

#### Scenario: Strict mode error
- **WHEN** 值包含 `{{text:secrets:NONEXISTENT}}`，该 key 不存在
- **THEN** 系统 SHALL 报错 `unresolved reference {{text:secrets:NONEXISTENT}}: key not found`

#### Scenario: Group not found in strict mode
- **WHEN** 值包含 `{{env:unknown_group:KEY}}`，group 不存在
- **THEN** 系统 SHALL 报错 `unresolved reference {{env:unknown_group:KEY}}: group not found`

### Requirement: Loose mode
指定 `--loose` flag 时，解引用 SHALL 为宽松模式：引用目标不存在时保留 `{{...}}` 原样，并在 stderr 输出警告。

#### Scenario: Loose mode preserves reference
- **WHEN** 用户执行 `senv env get NOTE -d --loose`，值包含 `{{text:secrets:MISSING}}`
- **THEN** 系统 SHALL 输出值中保留 `{{text:secrets:MISSING}}` 原样，并在 stderr 输出警告

### Requirement: Circular reference detection
解引用引擎 SHALL 检测循环引用。检测到循环时 MUST 报错并显示引用链。

#### Scenario: Direct circular reference
- **WHEN** env A = `{{env:default:B}}`，env B = `{{env:default:A}}`，用户执行 `senv env get A -d`
- **THEN** 系统 SHALL 报错 `circular reference detected: env:default:A → env:default:B → env:default:A`

#### Scenario: Indirect circular reference
- **WHEN** A → B → C → A（三级间接循环）
- **THEN** 系统 SHALL 报错并显示完整引用链 `A → B → C → A`

#### Scenario: Maximum recursion depth
- **WHEN** 引用嵌套超过 10 层
- **THEN** 系统 SHALL 报错 `maximum reference depth exceeded`

### Requirement: Recursive resolution
解引用 SHALL 递归展开：获取到的引用值如果包含进一步的引用，MUST 继续解析，直到无更多引用或达到最大深度。

#### Scenario: Nested references resolved
- **WHEN** env GREETING = `Hello {{env:NAME}}`，env NAME = `{{text:secrets:USER}}`，text USER = `Alice`
- **WHEN** 用户执行 `senv env get GREETING -d`
- **THEN** 系统 SHALL 输出 `Hello Alice`

#### Scenario: Mixed env/text references
- **WHEN** env URL = `postgres://{{text:secrets:USER}}:{{text:secrets:PASS}}@host/db`
- **WHEN** 用户执行 `senv env get URL -d`
- **THEN** 系统 SHALL 输出包含实际 USER 和 PASS 值的完整 URL

### Requirement: Reference resolver as shared module
引用解析 SHALL 作为独立模块 `internal/ref/` 实现。env 和 text 模块 SHALL 通过 `ValueGetter` 接口与 resolver 解耦。

#### Scenario: ValueGetter interface
- **WHEN** 引用解析器需要获取 env 或 text 的实际值
- **THEN** 解析器 SHALL 通过 `ValueGetter` 接口的 `GetEnvValue(group, key)` 和 `GetTextValue(group, key)` 方法获取，不直接依赖具体模块

### Requirement: env decode flags
`env get` 和 `env list` SHALL 支持 `-d`/`--decode` flag 进行解引用，以及 `--loose` flag 启用宽松模式。

#### Scenario: env get with decode
- **WHEN** 用户执行 `senv env get -d API_KEY`
- **THEN** 系统 SHALL 解引用后输出值

#### Scenario: env list with decode
- **WHEN** 用户执行 `senv env list -d`
- **THEN** 系统 SHALL 对所有值解引用后显示
