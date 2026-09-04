# env-key-validation Specification

## Purpose
定义 env 变量 key 的命名校验规则，确保所有写入的 key 都是合法的 POSIX shell 变量名，从而可被 `env export` 安全地输出为 `export NAME=value` 语句供 shell `eval` 执行，同时容错处理历史已存的非法 key。
## Requirements
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

### Requirement: env group 必须是安全单路径段

系统 SHALL 在创建、激活、停用、列出、读取、写入或删除 env group 前验证 group 非空、不是 `.` 或 `..`，且不含 NUL、`:`、`/`、`\\`、绝对路径或平台卷标语义。该校验 MUST 覆盖 CLI、快捷方式、TUI、MCP、同步和 storage 公开入口。

#### Scenario: AddGroup 拒绝路径穿越
- **WHEN** 任一调用方尝试创建名为 `../escaped` 的 env group
- **THEN** 系统返回非法 group 错误，data 根内外均不创建目录或文件

#### Scenario: 点目录和空 group 被拒绝
- **WHEN** group 为空、`.` 或 `..`
- **THEN** 系统在访问文件系统前拒绝操作

#### Scenario: Windows 分隔符被跨平台拒绝
- **WHEN** group 包含 `\\` 或卷标形式，即使当前平台不是 Windows
- **THEN** 系统仍拒绝该 group，避免同步到其他平台后改变路径语义

#### Scenario: 合法 group 正常使用
- **WHEN** group 为 `prod`、`my-group` 或 `team_1`
- **THEN** 系统按既有行为在受管 env 根内完成操作

### Requirement: env 存储入口同时验证 group 与 key

所有 env 存储入口 SHALL 在组合路径前同时验证安全 group 和现有 POSIX shell key 规则；任一失败 MUST 导致零文件变化。历史非法路径身份 MUST 报告为损坏数据，不得被静默跟随或跳过后继续执行破坏性操作。

#### Scenario: 合法 key 配合非法 group
- **WHEN** key 为 `API_KEY` 但 group 为绝对路径或 `..`
- **THEN** 写入被拒绝，合法 key 不绕过 group 校验

#### Scenario: 发现历史非法 group
- **WHEN** list、export 或 delete 枚举到无法证明位于 env 根内的历史 group
- **THEN** 系统报告该身份错误且不访问根外路径
