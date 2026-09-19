## MODIFIED Requirements

### Requirement: 禁止隐式创建 env/text 组

向不存在的 env、text 或 backup 组写入条目时，系统 MUST 失败并提示先显式创建组，MUST NOT 自动创建组目录或组元数据。该规则 MUST 覆盖 CLI、TUI、MCP 与 manager 公共写入入口（含 import、编辑器 set）。`default` 组在 vault 初始化时已存在（backup 的 `default` 亦可由 Manager 打开时幂等补建），向 `default` 写入 MUST 成功。

#### Scenario: env set 到不存在的组

- **WHEN** 用户执行 `senv env set -g newsvc FOO bar` 且 `newsvc` 不存在
- **THEN** 命令非 0 退出，不创建该组，不写入 FOO

#### Scenario: text set 到不存在的组

- **WHEN** 用户执行 `senv text set -g newnotes KEY val` 且 `newnotes` 不存在
- **THEN** 命令非 0 退出，不创建该组

#### Scenario: MCP env set 同样拒绝

- **WHEN** agent 调用 `senv_env_set` 写入不存在的组
- **THEN** 工具返回错误，vault 无新组

#### Scenario: 已有组可继续写入

- **WHEN** 组 `svc` 已存在
- **THEN** `senv env set -g svc FOO bar` 成功

#### Scenario: backup set 到不存在的组

- **WHEN** 用户执行 `senv backup set -g newnotes KEY val` 且 `newnotes` 不存在
- **THEN** 命令非 0 退出，不创建该组

### Requirement: 新建 env/text 组必填说明

`env group add`、`text group add` 与 `backup group add`（含 TUI `+`、MCP `senv_group_add`）MUST 要求非空说明（去空白后长度大于 0，且不超过说明上限）。缺说明或仅空白 MUST 拒绝且不创建组。存量组无说明 MUST 仍可列出、读取、写入条目、重命名与删除。

#### Scenario: CLI 带说明建组

- **WHEN** 用户执行 `senv env group add svc --description "自建服务存档，key 带服务前缀"`
- **THEN** 组被创建且 list 显示该说明

#### Scenario: 缺说明拒绝

- **WHEN** 用户执行 `senv text group add secrets` 且未提供说明
- **THEN** 命令非 0 退出，不创建目录

#### Scenario: MCP 建组缺说明拒绝

- **WHEN** agent 调用 `senv_group_add` 不传说明
- **THEN** 工具失败，不创建组

#### Scenario: 存量无说明组仍可写入

- **WHEN** 旧组 `feg` 无说明
- **THEN** 向其 set 条目成功，不必先补说明

#### Scenario: backup 缺说明拒绝

- **WHEN** 用户执行 `senv backup group add secrets` 且未提供说明
- **THEN** 命令非 0 退出，不创建目录
