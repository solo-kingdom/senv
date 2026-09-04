## ADDED Requirements

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
