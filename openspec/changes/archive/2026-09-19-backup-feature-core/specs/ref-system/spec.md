## MODIFIED Requirements

### Requirement: Reference syntax

系统 SHALL 支持在 env 和 text 的值中嵌入引用模板。引用格式为 `{{type:key}}` 或 `{{type:group:key}}`，其中 type MUST 为 `env` 或 `text`。`backup` MUST NOT 成为合法 type。不含类型前缀的 `{{...}}` SHALL 视为原文本，不做解析。`\{{` SHALL 作为转义，输出字面 `{{` 而不触发解析。

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

#### Scenario: backup type is not a reference

- **WHEN** 值包含 `{{backup:notes:DUMP}}` 且 backup 条目 `notes:DUMP` 存在
- **THEN** 系统 MUST NOT 用该 backup 值替换模板（结果不得等于 backup 正文）
