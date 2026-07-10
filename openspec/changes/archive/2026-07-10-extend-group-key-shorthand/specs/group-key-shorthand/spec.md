## MODIFIED Requirements

### Requirement: group:key 地址语法作为 set 快捷方式
系统 SHALL 支持 `group:key` 地址格式作为 env/text 操作的快捷寻址方式。地址中 `:` 为必要分隔符，左侧为 group，右侧为 key；左侧为空时 group 默认为 `default`，右侧为空时 key 默认为 `__default`。该语法适用于：根命令与 `text`/`env` 父命令的 set 快捷方式，以及 `text get`/`text delete`/`text set`、`env get`/`env delete`/`env set` 子命令的位置参数。

#### Scenario: 完整 group:key 快捷写入（根命令）
- **WHEN** 用户运行 `senv mygroup:mykey myvalue`
- **THEN** 系统将 `myvalue` 写入 text group `mygroup` 的 key `mykey`，等价于 `senv text set -g mygroup mykey myvalue`

#### Scenario: 仅 key，省略 group（使用默认 group）
- **WHEN** 用户运行 `senv :mykey myvalue`
- **THEN** 系统将 `myvalue` 写入 text group `default` 的 key `mykey`

#### Scenario: 仅 group，省略 key（使用默认 key）
- **WHEN** 用户运行 `senv mygroup:`
- **THEN** 系统打开编辑器编辑 text group `mygroup` 的 key `__default`

#### Scenario: 仅冒号（group 和 key 均省略）
- **WHEN** 用户运行 `senv : myvalue`
- **THEN** 系统将 `myvalue` 写入 text group `default` 的 key `__default`

#### Scenario: text 子命令快捷方式
- **WHEN** 用户运行 `senv text mygroup:mykey myvalue`
- **THEN** 系统将 `myvalue` 写入 text group `mygroup` 的 key `mykey`

#### Scenario: env 子命令快捷方式（有 value）
- **WHEN** 用户运行 `senv env mygroup:mykey myvalue`
- **THEN** 系统将 `myvalue` 写入 env group `mygroup` 的 key `mykey`，等价于 `senv env set -g mygroup mykey myvalue`

#### Scenario: env 子命令快捷方式（无 value）
- **WHEN** 用户运行 `senv env mygroup:mykey`（不提供 value）
- **THEN** 系统返回错误，提示 env set 需要提供值

#### Scenario: 无 address 时不触发快捷方式
- **WHEN** 用户运行 `senv text`（无任何参数）
- **THEN** 系统显示 `text` 命令的帮助信息，不执行任何 set 操作

#### Scenario: 参数不含冒号时不触发快捷方式
- **WHEN** 用户运行 `senv hello`（无冒号）
- **THEN** 系统显示帮助或报 unknown command 错误，不触发快捷方式逻辑

#### Scenario: text get 使用 group:key
- **WHEN** 用户运行 `senv text get feg:ACCOUNT`
- **THEN** 系统从 text group `feg` 读取 key `ACCOUNT`，等价于 `senv text get -g feg ACCOUNT`

#### Scenario: text delete 使用 group:key
- **WHEN** 用户运行 `senv text delete feg:ACCOUNT`
- **THEN** 系统删除 text group `feg` 的 key `ACCOUNT`，等价于 `senv text delete -g feg ACCOUNT`

#### Scenario: text set 子命令使用 group:key
- **WHEN** 用户运行 `senv text set feg:ACCOUNT myvalue`
- **THEN** 系统将 `myvalue` 写入 text group `feg` 的 key `ACCOUNT`，等价于 `senv text set -g feg ACCOUNT myvalue`

#### Scenario: env get 使用 group:key
- **WHEN** 用户运行 `senv env get feg:API_KEY`
- **THEN** 系统从 env group `feg` 读取 key `API_KEY`，等价于 `senv env get -g feg API_KEY`

#### Scenario: env delete 使用 group:key
- **WHEN** 用户运行 `senv env delete feg:API_KEY`
- **THEN** 系统删除 env group `feg` 的 key `API_KEY`，等价于 `senv env delete -g feg API_KEY`

#### Scenario: env set 子命令使用 group:key
- **WHEN** 用户运行 `senv env set feg:API_KEY myvalue`
- **THEN** 系统将 `myvalue` 写入 env group `feg` 的 key `API_KEY`，等价于 `senv env set -g feg API_KEY myvalue`

#### Scenario: 快捷方式与完整命令访问同一 entry（读取）
- **WHEN** 用户先通过 `senv mygroup: value1` 写入，再通过 `senv text get mygroup:__default` 读取
- **THEN** 读取结果为 `value1`

## ADDED Requirements

### Requirement: address 参数优先于 -g flag
当位置参数含 `:` 并被解析为 address 时，系统 SHALL 使用 address 中的 group，忽略同时指定的 `-g`/`--group` flag。

#### Scenario: address 覆盖 -g flag
- **WHEN** 用户运行 `senv text get -g other feg:ACCOUNT`
- **THEN** 系统从 text group `feg` 读取 key `ACCOUNT`，不使用 `other` 分组

#### Scenario: 无冒号时使用 -g flag
- **WHEN** 用户运行 `senv text get -g feg ACCOUNT`
- **THEN** 系统从 text group `feg` 读取 key `ACCOUNT`
