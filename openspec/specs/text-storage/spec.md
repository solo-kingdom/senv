## ADDED Requirements

### Requirement: Text CRUD with group support
系统 SHALL 提供 `senv text` 命令组，通过 `-g` flag 指定 group（默认 `default`），支持以下子命令：`set`、`get`、`delete`、`list`。group SHALL 作为目录名（`dataPath/texts/{group}/`），每个 text 条目 SHALL 存储为独立加密文件（`{key}.enc`）。

#### Scenario: Set text with inline value
- **WHEN** 用户执行 `senv text -g notes set README "hello world"`
- **THEN** 系统 SHALL 将 "hello world" 加密存储到 `dataPath/texts/notes/README.enc`

#### Scenario: Set text from file
- **WHEN** 用户执行 `senv text -g keys set SSH --file ~/.ssh/id_rsa`
- **THEN** 系统 SHALL 读取文件内容，加密存储到 `dataPath/texts/keys/SSH.enc`

#### Scenario: Set text from stdin
- **WHEN** 用户执行 `echo "content" | senv text -g notes set MEMO`
- **THEN** 系统 SHALL 从 stdin 读取内容，加密存储到 `dataPath/texts/notes/MEMO.enc`

#### Scenario: Set text via editor (new key)
- **WHEN** 用户执行 `senv text -g notes set LOG`（无 value、无 pipe、无 --file）
- **THEN** 系统 SHALL 打开编辑器（`$VISUAL` > `$EDITOR` > `nano` > `vim`），用户编辑保存退出后，内容加密存储

#### Scenario: Set text via editor (existing key)
- **WHEN** 用户执行 `senv text -g notes set README`（key 已存在）
- **THEN** 系统 SHALL 打开编辑器并预填现有内容

#### Scenario: Get text
- **WHEN** 用户执行 `senv text -g notes get README`
- **THEN** 系统 SHALL 解密并输出原始文本到 stdout

#### Scenario: Get text to file
- **WHEN** 用户执行 `senv text -g notes get README -o /tmp/readme.txt`
- **THEN** 系统 SHALL 将解密内容写入指定文件

#### Scenario: Get text to clipboard
- **WHEN** 用户执行 `senv text -g notes get README --copy`
- **THEN** 系统 SHALL 将解密内容复制到系统剪贴板

#### Scenario: Delete text
- **WHEN** 用户执行 `senv text -g notes delete README`
- **THEN** 系统 SHALL 删除 `dataPath/texts/notes/README.enc` 文件

#### Scenario: List texts in group
- **WHEN** 用户执行 `senv text -g notes list`
- **THEN** 系统 SHALL 显示该 group 下所有 key 的元信息（key 名、大小、更新时间）

### Requirement: Text value size limit
系统 SHALL 限制单个 text 值不超过 512KB。超过限制时 MUST 报错并拒绝存储。

#### Scenario: Value exceeds limit
- **WHEN** 用户尝试存储超过 512KB 的文本
- **THEN** 系统 SHALL 报错 `text value exceeds 512KB limit (<actual> bytes)` 并拒绝存储

#### Scenario: Value within limit
- **WHEN** 用户存储恰好 512KB 的文本
- **THEN** 系统 SHALL 正常存储

### Requirement: Text group management
系统 SHALL 提供 `senv text group` 子命令，支持 `list`、`add`、`delete`。text group 不需要 activate/deactivate 机制。删除 group 时 MUST 要求用户确认。

#### Scenario: Add group
- **WHEN** 用户执行 `senv text group add secrets`
- **THEN** 系统 SHALL 创建 `dataPath/texts/secrets/` 目录

#### Scenario: List groups
- **WHEN** 用户执行 `senv text group list`
- **THEN** 系统 SHALL 列出所有 text group 及其 key 数量

#### Scenario: Delete group with confirmation
- **WHEN** 用户执行 `senv text group delete secrets`
- **THEN** 系统 SHALL 提示确认，确认后删除该目录及所有内容

#### Scenario: Delete group cancelled
- **WHEN** 用户在确认提示时选择取消
- **THEN** 系统 SHALL 不做任何删除操作

### Requirement: Text encrypted file format
每个 text 加密文件解密后 SHALL 为 JSON 格式，包含 `value`（实际文本）、`size`（字节数）、`created_at`（ISO 8601）、`updated_at`（ISO 8601）。

#### Scenario: File format on creation
- **WHEN** 用户首次创建 text 条目
- **THEN** 解密后的 JSON SHALL 包含 value、size、created_at、updated_at，其中 created_at 等于 updated_at

#### Scenario: File format on update
- **WHEN** 用户更新已有 text 条目
- **THEN** 解密后的 JSON SHALL 更新 value、size、updated_at，created_at 保持不变

### Requirement: Editor temporary file security
系统在打开编辑器时 SHALL 将内容写入临时文件，权限设为 0600，编辑完成后 SHALL 立即删除临时文件。

#### Scenario: Temp file cleanup on success
- **WHEN** 用户在编辑器中编辑并保存
- **THEN** 系统 SHALL 在读取内容后立即删除临时文件

#### Scenario: Temp file cleanup on editor error
- **WHEN** 编辑器进程异常退出
- **THEN** 系统 SHALL 仍通过 defer 删除临时文件

### Requirement: Set input priority
`senv text set` 的输入源 SHALL 按以下优先级处理：`--file` > stdin pipe > 命令行参数 > 编辑器。

#### Scenario: --file takes priority
- **WHEN** 用户同时提供 `--file` 和命令行 value 参数
- **THEN** 系统 SHALL 使用 --file 的内容

#### Scenario: Stdin takes priority over editor
- **WHEN** stdin 是 pipe（非终端），且无 --file 和命令行 value
- **THEN** 系统 SHALL 从 stdin 读取，不打开编辑器

#### Scenario: Editor as fallback
- **WHEN** 无 --file、无 stdin pipe、无命令行 value
- **THEN** 系统 SHALL 打开编辑器

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

### Requirement: Text 明文文件导出安全

`text get -o` 与 TUI text 导出 SHALL 在写入前展开 `~`，使用平台路径语义解析 basename、相对子目录和绝对路径，并以安全原子写输出明文。默认 mode MUST 为 0600；覆盖既有普通文件时 MUST 收紧至 0600，除非用户在该次 CLI 操作中显式指定其他受支持 mode。

#### Scenario: basename 导出
- **WHEN** 用户执行 `senv text get secrets:PRIVATE_KEY -o key.pem`
- **THEN** 系统在当前目录创建 0600 的 `key.pem`，不 panic，也不尝试创建空目录

#### Scenario: 相对和绝对路径导出
- **WHEN** 输出为相对子目录或绝对路径且父目录不存在
- **THEN** 系统创建仅当前用户可访问的必要父目录，并原子写入 0600 文件

#### Scenario: home 路径导出
- **WHEN** 输出路径使用 `~/keys/id.pem`
- **THEN** 系统先展开当前用户主目录，再写入预期位置，不创建名为 `~` 的目录

#### Scenario: 覆盖宽松文件
- **WHEN** 默认导出覆盖既有 0644 普通文件
- **THEN** 写入后文件内容完整且权限收紧为 0600

#### Scenario: 目标或父目录是符号链接
- **WHEN** 输出目标或从可信父目录到目标的路径包含符号链接
- **THEN** 导出被拒绝，链接目标内容保持不变

### Requirement: Text 导出 mode 必须显式且有效

CLI SHALL 提供显式 mode 选项用于放宽明文输出权限。系统 MUST 验证 mode 是受支持的八进制普通文件权限；非法、包含特殊位或超出允许范围的值 MUST 在写入前被拒绝。TUI 未提供 mode 时 SHALL 始终使用 0600。

#### Scenario: 显式导出 0644
- **WHEN** 用户明确指定 `--mode 0644` 导出 text
- **THEN** 系统按 0644 写入，并不把该选择持久化为后续默认值

#### Scenario: 非法 mode
- **WHEN** 用户指定无法解析、包含 setuid/setgid/sticky 位或非普通权限范围的 mode
- **THEN** 命令返回参数错误，不创建或修改输出文件
