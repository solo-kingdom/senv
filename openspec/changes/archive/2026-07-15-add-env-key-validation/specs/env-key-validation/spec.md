## ADDED Requirements

### Requirement: env key 必须是合法的 shell 变量名
系统 SHALL 在写入 env 变量时校验 key 名称：key 必须匹配 POSIX shell 变量名规则 `^[A-Za-z_][A-Za-z0-9_]*$`（以字母或下划线开头，后续仅允许字母、数字、下划线）。不合法的 key SHALL 被拒绝并返回包含违规 key 原值与命名规则说明的错误，且不得落盘。该校验 SHALL 在 `env.Manager.Set` 中统一执行，覆盖所有写入入口（CLI `env set`、根命令与 `env` 父命令的 `group:key` 快捷方式、交互式菜单、TUI）。

#### Scenario: 合法的字母下划线 key 被接受
- **WHEN** 用户运行 `senv env set API_KEY secret`
- **THEN** 系统成功写入变量 `API_KEY`

#### Scenario: 下划线开头的 key 被接受
- **WHEN** 用户运行 `senv env set _PRIVATE value`
- **THEN** 系统成功写入变量 `_PRIVATE`

#### Scenario: 含斜杠的 key 被拒绝
- **WHEN** 用户运行 `senv env set openviking/root_api_key xxx`
- **THEN** 系统返回错误，提示 `openviking/root_api_key` 不是合法的 shell 变量名，且不写入任何数据

#### Scenario: 以数字开头的 key 被拒绝
- **WHEN** 用户运行 `senv env set 123KEY value`
- **THEN** 系统返回错误，提示该 key 非法，且不写入任何数据

#### Scenario: 含连字符的 key 被拒绝
- **WHEN** 用户运行 `senv env set my-key value`
- **THEN** 系统返回错误，提示 `my-key` 不是合法的 shell 变量名

#### Scenario: 含点号的 key 被拒绝
- **WHEN** 用户运行 `senv env set foo.bar value`
- **THEN** 系统返回错误，提示 `foo.bar` 不是合法的 shell 变量名

#### Scenario: group:key 快捷方式下非法 key 被拒绝
- **WHEN** 用户运行 `senv env prod:my/key value`
- **THEN** 系统按 `:` 拆分得到 key `my/key`，因其含 `/` 而返回错误，且不写入任何数据

#### Scenario: group:key 快捷方式下合法 key 被接受
- **WHEN** 用户运行 `senv env prod:API_KEY secret`
- **THEN** 系统按 `:` 拆分得到 key `API_KEY`，校验通过并成功写入 env group `prod`

#### Scenario: 空字符串 key 被拒绝
- **WHEN** 上层逻辑向 `env.Manager.Set` 传入空字符串 key
- **THEN** 系统返回错误，提示该 key 非法，且不写入任何数据

### Requirement: export 容错历史非法 key
系统 SHALL 在 `env export` 时对每个 key 进行同名校验：合法 key 正常输出 `export` 语句；非法 key SHALL 被跳过（不输出对应 export 行）并向标准错误输出包含违规 key 原值与所属 group 的警告，且不得中断其余合法变量的导出。

#### Scenario: export 跳过历史非法 key 并警告
- **WHEN** env group `default` 中存在历史非法 key `openviking/root_api_key` 与合法 key `API_KEY`，用户运行 `senv env export`
- **THEN** 系统向标准错误输出 `warning: skipping invalid env key "openviking/root_api_key" in group "default" ...`，并在标准输出仅包含 `export API_KEY='...'`，不输出非法 key 的 export 行

#### Scenario: export 全部合法时无警告
- **WHEN** 所有激活 group 中的 key 均合法，用户运行 `senv env export`
- **THEN** 系统输出所有合法变量的 export 语句，且不向标准错误输出任何 warning
