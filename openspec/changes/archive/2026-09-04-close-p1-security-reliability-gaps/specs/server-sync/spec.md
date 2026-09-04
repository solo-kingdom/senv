## ADDED Requirements

### Requirement: 同步条目标识遵守严格 schema

server 与客户端 SHALL 仅接受已知 kind，并按 kind 校验 grp/key：`env` 要求 grp/key 均为安全单路径段；`env_meta` 要求 grp 为安全单路径段且 key 为空；`text` 要求 grp/key 均为安全单路径段；`config` 要求 grp 为空且 key 为安全单路径段；`config_index` 要求 grp/key 均为空。未知 kind、缺失字段、额外身份字段或不安全路径段 MUST 被拒绝。

#### Scenario: 五种合法 kind 被接受
- **WHEN** push 或 pull 条目分别满足 `env`、`env_meta`、`text`、`config`、`config_index` 的身份 schema
- **THEN** 条目继续按正常同步流程处理

#### Scenario: kind 未知
- **WHEN** 条目的 kind 不在已知白名单中
- **THEN** server 拒绝整批 push，客户端拒绝 apply，且不创建任何本地文件

#### Scenario: kind 字段组合非法
- **WHEN** `config` 携带 grp、`env_meta` 携带 key，或要求的 grp/key 为空
- **THEN** 条目在访问文件系统前被拒绝

#### Scenario: 身份包含路径语义
- **WHEN** grp 或 key 为 `../x`、`a/../../x`、绝对路径、含 `/`、`\\`、NUL、`.` 或 `..`
- **THEN** server 和客户端均返回验证错误，条目不会进入持久化或本地缓存

### Requirement: 客户端不信任远端同步身份

客户端 SHALL 在每次将远端条目映射为路径、写入或删除前独立执行 schema、containment 和无符号链接验证，不得依赖 server 已校验。无效 pull 批次 MUST NOT 更新本地文件、metadata 或同步 revision/state。

#### Scenario: 被攻陷 server 返回穿越条目
- **WHEN** pull 响应包含 `kind=config,key=../escaped`
- **THEN** 客户端终止该批 apply，data/config 根内外均无文件变化，last synced revision 不前进

#### Scenario: 远端删除指向符号链接
- **WHEN** 删除条目的本地目标或父目录是符号链接
- **THEN** 客户端拒绝删除，链接及其目标保持不变，同步状态不提交

### Requirement: server 在事务前验证完整批次

server SHALL 在创建 vault、推进 revision 或写入任一条目前验证 push 批次内全部 entry identity；任一条目无效时 MUST 原子拒绝整批。

#### Scenario: 批次包含一个非法条目
- **WHEN** push 批次同时包含合法条目和一个非法 identity
- **THEN** server 返回可理解的验证错误，不创建 vault、不推进 revision、不写入任何条目
