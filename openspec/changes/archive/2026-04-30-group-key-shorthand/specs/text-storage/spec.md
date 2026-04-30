## ADDED Requirements

### Requirement: key 名禁止包含冒号
系统 SHALL 拒绝包含 `:` 字符的 key 名，在 text 和 env 的存储层 Set 方法中校验。

#### Scenario: 写入含冒号的 key 返回错误
- **WHEN** 用户通过任何命令尝试写入 key 名包含 `:` 的 entry（如 `foo:bar`）
- **THEN** 系统返回错误，提示 key 名不能包含 `:`，拒绝写入

#### Scenario: 合法 key 名正常写入
- **WHEN** 用户写入 key 名不含 `:` 的 entry（如 `mykey`、`my-key`、`my_key`）
- **THEN** 系统正常写入，不报错

### Requirement: group 名禁止包含冒号
系统 SHALL 拒绝包含 `:` 字符的 group 名，在 text 和 env 的存储层中校验。

#### Scenario: 写入含冒号的 group 返回错误
- **WHEN** 用户尝试在 group 名包含 `:` 的分组中写入 entry
- **THEN** 系统返回错误，提示 group 名不能包含 `:`，拒绝写入

#### Scenario: 合法 group 名正常写入
- **WHEN** 用户在 group 名不含 `:` 的分组中写入 entry（如 `prod`、`my-group`）
- **THEN** 系统正常写入，不报错
