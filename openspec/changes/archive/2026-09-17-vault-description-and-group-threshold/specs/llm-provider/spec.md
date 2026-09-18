## ADDED Requirements

### Requirement: LLM Provider 档案说明
`senv ai provider add` 与 `edit` SHALL 接受可选 `--description`。`list`/`show` SHALL 展示档案说明。说明属于 Provider 档案，MUST NOT 写入或覆盖 `model_info` 中的模型目录文案。空说明合法；长度遵循 vault-description。

#### Scenario: add with description
- **WHEN** 用户 `senv ai provider add api --description "公网 token 网关" ...`（其余必填项合法）
- **THEN** 档案保存该说明，`show` 可见

#### Scenario: edit description only
- **WHEN** 用户 `senv ai provider edit api --description "内网网关"`
- **THEN** 仅说明更新，模型集与凭据引用不变

#### Scenario: model catalog text unchanged
- **WHEN** 档案说明被更新，某模型已有目录简介
- **THEN** 该模型简介保持原值
