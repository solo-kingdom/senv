## Purpose

提供以分组为目录的加密备份存储：CLI `senv backup` 的 set/get/delete/list/import/export 与 group 管理，以及 MCP `senv_backup_*` 工具。backup 与 text 隔离，不参与 `{{…}}` 引用，不占用根快捷 `senv <group:key>`。

## ADDED Requirements

### Requirement: Backup CRUD with group support

系统 SHALL 提供 `senv backup` 命令组，通过 `-g` flag 指定 group（默认 `default`），支持 `set`、`get`、`delete`、`list`、`import`、`export`。group SHALL 作为目录名（`dataPath/backups/{group}/`），每个条目 SHALL 存储为独立加密文件（`{key}.enc`）。地址 `group:key` 优先于 `-g`。`import` SHALL 从必填 `--file` 读取并加密入库（源文件不动）；已存在 key 时 SHALL 覆盖并刷新 `updated_at`，无确认。`set` 输入优先级 SHALL 为 `--file` > stdin 管道 > 参数 > 编辑器。

#### Scenario: Set backup with inline value

- **WHEN** 用户执行 `senv backup -g notes set DUMP "hello"`
- **THEN** 系统 SHALL 将内容加密存储到 `dataPath/backups/notes/DUMP.enc`

#### Scenario: Set backup from file

- **WHEN** 用户执行 `senv backup set notes:DUMP --file ./dump.txt`
- **THEN** 系统 SHALL 读取文件内容并加密入库

#### Scenario: Import over existing key

- **WHEN** `notes:DUMP` 已存在，用户执行 `senv backup import notes:DUMP --file ./dump.txt`
- **THEN** 系统 SHALL 覆盖现有值并刷新 `updated_at`，不提示确认

#### Scenario: Get backup

- **WHEN** 用户执行 `senv backup get notes:DUMP`
- **THEN** 系统 SHALL 解密并输出原始字节到 stdout，MUST NOT 解析 `{{…}}` 引用

#### Scenario: Get backup has no decode flag

- **WHEN** 用户执行 `senv backup get --help`
- **THEN** 帮助 MUST NOT 提供 `-d/--decode` 或 `--loose`

#### Scenario: List backups in group

- **WHEN** 用户执行 `senv backup list notes`
- **THEN** 系统 SHALL 显示该组 key、大小、更新时间与说明，MUST NOT 输出 value

### Requirement: Backup value size limit

系统 SHALL 限制单个 backup 值不超过 512KB（`MaxBackupSize`，只计 value 明文）。超过时 MUST 报错并拒绝存储，MUST NOT 截断。description 与加密信封不计上限。恰好 512KB SHALL 成功。

#### Scenario: Value exceeds limit

- **WHEN** 用户尝试存储超过 512KB 的 backup value
- **THEN** 系统 SHALL 报错并拒绝存储，vault 无新文件或无半写入文件

#### Scenario: Value at limit

- **WHEN** 用户存储恰好 512KB 的 backup value
- **THEN** 系统 SHALL 正常存储

### Requirement: Backup group management

系统 SHALL 提供 `senv backup group` 的 `list`、`add`、`delete`。backup 组无 activate/deactivate。`add` MUST 要求非空说明。删除组 MUST 交互确认。`list` SHALL 展示说明与 key 数量。

#### Scenario: Add group without description

- **WHEN** 用户执行 `senv backup group add secrets` 且未提供说明
- **THEN** 系统 MUST 拒绝，不创建组

#### Scenario: Delete group cancelled

- **WHEN** 用户在确认提示时取消
- **THEN** 系统 SHALL 不做任何删除

### Requirement: Backup encrypted file format

每个 backup 加密文件解密后 SHALL 为 JSON，含 `value`、`size`、`created_at`、`updated_at`，以及可选 `description`。更新值且未传说明时 MUST 保留原说明与 `created_at`。

#### Scenario: File format on update

- **WHEN** 用户更新已有 backup 值且未改说明
- **THEN** 解密 JSON SHALL 更新 value、size、updated_at，created_at 与说明不变

### Requirement: Backup set 不隐式建组

`backup set` / `import` 在目标组不存在时 MUST 失败，MUST NOT 创建组目录。`default` 组在 init 或 Manager 打开时 SHALL 已存在，向 `default` 写入 MUST 成功。

#### Scenario: set into missing group

- **WHEN** 组 `nope` 不存在，用户执行 `senv backup set -g nope KEY val`
- **THEN** 操作失败，不创建 `backups/nope/`

### Requirement: Backup 明文导出安全

`backup get -o`、`backup export --path` 与 TUI backup 导出 SHALL 对齐 text 的落盘安全：展开 `~`、默认 0600、覆盖收紧、拒绝符号链接。`export` 内容为解密原文、不经引用解析；成功只打印路径。`--path` 必填。

#### Scenario: CLI export

- **WHEN** 用户执行 `senv backup export notes:DUMP --path dump.txt`
- **THEN** 系统 SHALL 原子写入 0600 文件，输出仅提示路径

### Requirement: Backup MCP 工具

系统 SHALL 提供 MCP 工具 `senv_backup_get`、`senv_backup_set`、`senv_backup_delete`、`senv_backup_list`。`list` MUST NOT 返回 value；`get`/`set`/`delete` 可含正文。`senv_group_add` / `senv_group_list` SHALL 接受 `kind=backup`。无保留组封锁。省略 `description` 时 set MUST 保留原说明。

#### Scenario: MCP list hides value

- **WHEN** agent 调用 `senv_backup_list`
- **THEN** 结果含 key/size/description，不含 value

#### Scenario: MCP set into missing group

- **WHEN** agent 调用 `senv_backup_set` 写入不存在的组
- **THEN** 工具返回错误，不创建组

### Requirement: Backup 不占用根快捷

根命令 `senv <group:key> [value]` SHALL 仍写入 text，MUST NOT 写入 backup。

#### Scenario: root shorthand stays text

- **WHEN** 用户执行 `senv notes:TODO "x"`
- **THEN** 内容写入 `texts/notes/TODO.enc`，MUST NOT 写入 `backups/`
