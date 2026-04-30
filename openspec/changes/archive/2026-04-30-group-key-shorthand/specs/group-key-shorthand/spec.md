## ADDED Requirements

### Requirement: group:key 地址语法作为 set 快捷方式
系统 SHALL 支持 `group:key` 地址格式作为 `text set`（默认）和 `env set` 命令的快捷方式。地址中 `:` 为必要分隔符，左侧为 group，右侧为 key；左侧为空时 group 默认为 `default`，右侧为空时 key 默认为 `__default`。

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

### Requirement: 快捷方式支持 --file 选项（text 类型）
text 类型的快捷方式 SHALL 支持 `--file` 选项，从文件中读取内容写入。

#### Scenario: 根命令快捷方式使用 --file
- **WHEN** 用户运行 `senv mygroup:mykey --file /path/to/cert.pem`
- **THEN** 系统读取文件内容并写入 text group `mygroup` 的 key `mykey`

#### Scenario: text 子命令快捷方式使用 --file
- **WHEN** 用户运行 `senv text mygroup:mykey --file /path/to/cert.pem`
- **THEN** 系统读取文件内容并写入 text group `mygroup` 的 key `mykey`

### Requirement: __default key 无特殊保留语义
`__default` SHALL 是普通 key 名，快捷方式（`group:`）与完整命令（`senv text set -g group __default`）访问同一存储 entry。

#### Scenario: 快捷方式与完整命令访问同一 entry
- **WHEN** 用户先通过 `senv mygroup: value1` 写入，再通过 `senv text get -g mygroup __default` 读取
- **THEN** 读取结果为 `value1`
