## MODIFIED Requirements

### Requirement: Text CRUD with group support
系统 SHALL 提供 `senv text` 命令组，通过 `-g` flag 指定 group（默认 `default`），支持以下子命令：`set`、`get`、`delete`、`list`、`import`、`export`。group SHALL 作为目录名（`dataPath/texts/{group}/`），每个 text 条目 SHALL 存储为独立加密文件（`{key}.enc`）。`import` SHALL 从 `--file` 读取文件内容加密入库（源文件保持不动）；目标 key 已存在时 SHALL 覆盖现有值并刷新 `updated_at`（与 TUI 文件导入同语义）；`--file` 为必填项，缺失时 MUST 报错且不回落 stdin/编辑器。

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

#### Scenario: Import text from file
- **WHEN** 用户执行 `senv text import notes:README --file ./README.md`
- **THEN** 系统 SHALL 读取文件内容加密存储到 `dataPath/texts/notes/README.enc`，源文件保持不动

#### Scenario: Import over existing key
- **WHEN** `notes:README` 已存在，用户执行 `senv text import notes:README --file ./README.md`
- **THEN** 系统 SHALL 覆盖现有值为文件内容并刷新 `updated_at`，不提示确认

#### Scenario: Import with nonexistent file
- **WHEN** 用户执行 `senv text import notes:README --file /no/such/file.md`
- **THEN** 系统 SHALL 报错且不产生任何存储变更

### Requirement: Text 明文文件导出安全

`text get -o`、`text export` 与 TUI text 导出 SHALL 在写入前展开 `~`，使用平台路径语义解析 basename、相对子目录和绝对路径，并以安全原子写输出明文。默认 mode MUST 为 0600；覆盖既有普通文件时 MUST 收紧至 0600，除非用户在该次 CLI 操作中显式指定其他受支持 mode。

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

#### Scenario: CLI export 命令导出明文
- **WHEN** 用户执行 `senv text export secrets:PRIVATE_KEY --path key.pem`
- **THEN** 系统 SHALL 在当前目录原子写入 0600 的 `key.pem`，内容 SHALL 为 vault 解密值的逐字节原文（不经引用解析），输出仅提示路径、MUST NOT 回显明文内容

#### Scenario: CLI export 不存在的 key
- **WHEN** 用户执行 `senv text export notes:NOPE --path out.txt`，key `NOPE` 不存在
- **THEN** 系统 SHALL 报错且不创建 `out.txt`
