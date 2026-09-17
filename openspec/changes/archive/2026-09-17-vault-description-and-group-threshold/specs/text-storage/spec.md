## MODIFIED Requirements

### Requirement: Text group management
系统 SHALL 提供 `senv text group` 子命令，支持 `list`、`add`、`delete`。text group 不需要 activate/deactivate 机制。删除 group 时 MUST 要求用户确认。`add` MUST 要求非空说明。`list` SHALL 展示每组说明（可为空）与 key 数量。

#### Scenario: Add group
- **WHEN** 用户执行 `senv text group add secrets --description "凭据存档"`
- **THEN** 系统 SHALL 创建该 text 组并保存说明

#### Scenario: Add group without description
- **WHEN** 用户执行 `senv text group add secrets` 且未提供说明
- **THEN** 系统 MUST 拒绝，不创建组

#### Scenario: List groups
- **WHEN** 用户执行 `senv text group list`
- **THEN** 系统 SHALL 列出所有 text group、说明与 key 数量

#### Scenario: Delete group with confirmation
- **WHEN** 用户执行 `senv text group delete secrets`
- **THEN** 系统 SHALL 提示确认，确认后删除该组及所有内容

#### Scenario: Delete group cancelled
- **WHEN** 用户在确认提示时选择取消
- **THEN** 系统 SHALL 不做任何删除操作

### Requirement: Text encrypted file format
每个 text 加密文件解密后 SHALL 为 JSON 格式，包含 `value`（实际文本）、`size`（字节数）、`created_at`（ISO 8601）、`updated_at`（ISO 8601），以及可选 `description`。缺省 description 视为空。

#### Scenario: File format on creation
- **WHEN** 用户首次创建 text 条目且未提供说明
- **THEN** 解密后的 JSON SHALL 包含 value、size、created_at、updated_at；description 为空或省略

#### Scenario: File format on update
- **WHEN** 用户更新已有 text 条目的值且未改说明
- **THEN** 解密后的 JSON SHALL 更新 value、size、updated_at，created_at 与说明保持不变

#### Scenario: Description persisted
- **WHEN** 用户为 text 条目设置说明
- **THEN** 解密 JSON 含该 description，value 不变

## ADDED Requirements

### Requirement: Text set 不隐式建组
`text set` / `import` 在目标组不存在时 MUST 失败，MUST NOT 创建组目录。

#### Scenario: set into missing group
- **WHEN** 组 `nope` 不存在，用户执行 `senv text set -g nope KEY val`
- **THEN** 操作失败，不创建 `texts/nope/`

### Requirement: Text list 含说明
`senv text list` 与 MCP `senv_text_list` SHALL 展示每条说明（可为空），仍 MUST NOT 输出 value。

#### Scenario: list shows description
- **WHEN** 条目 `notes:README` 有说明「周报草稿」
- **THEN** list 行含该说明，不含正文
