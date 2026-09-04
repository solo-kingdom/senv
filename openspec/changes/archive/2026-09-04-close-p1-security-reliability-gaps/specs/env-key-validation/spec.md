## ADDED Requirements

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
