# skill-storage Specification

## Purpose
TBD - created by archiving change senv-skills-config-core. Update Purpose after archive.

## Requirements

### Requirement: skill 同步身份

同步 schema SHALL 支持 kind `skill`（grp 为分组、key 为 skill id，二者皆须是合法单段标识）与 `skill_index`（grp、key 均为空）。client 与 server MUST 共用同一 `ValidateIdentity`；身份非法的远端条目 MUST 被拒绝而不落盘。

#### Scenario: skill 条目身份合法
- **WHEN** 校验 kind=skill、grp=`default`、key=`demo`
- **THEN** 通过

#### Scenario: 路径攻击被拒
- **WHEN** skill 的 grp 或 key 含 `..`、`/`、`\` 或 `:`
- **THEN** 返回 invalid identity

#### Scenario: skill_index 带 grp 或 key
- **WHEN** 校验 kind=skill_index 且 grp 或 key 非空
- **THEN** 返回 invalid identity

### Requirement: skill 存储布局与大小

skill 正文 SHALL 存为 `skills/<group>/<id>.enc`（ustar tar 明文经 vault 密钥加密），索引 SHALL 存为数据目录顶层明文 `skill_index.json`。tar 明文 MUST NOT 超过 1 MiB，超限错误 MUST 点名上限。skill id MUST 在 vault 内全局唯一（跨 group 不可重名）。

#### Scenario: 明文超限
- **WHEN** 保存超过 1 MiB 的 skill 归档
- **THEN** 拒绝，错误信息含 1 MiB 与字节数上限，且不写入任何文件

#### Scenario: 跨分组重名
- **WHEN** 在 group B 添加已存在于 group A 的 id（未 `--force`）
- **THEN** 拒绝并说明 id 跨分组唯一

#### Scenario: 索引损坏失败关闭
- **WHEN** `skill_index.json` 的条目 id 与键不一致、group 非法或 encrypted_file 非规范路径
- **THEN** 读取索引失败，不返回部分结果

### Requirement: skill 归档白名单

入库归档 MUST 以 skill 目录内容为 tar 根，且根下存在普通文件 `SKILL.md`，其 frontmatter `name` MUST 等于 skill id。归档条目类型 MUST 仅为普通文件（TypeReg/TypeRegA）或目录（TypeDir）；符号链接、硬链接、FIFO、设备、pax/GNU 扩展与未知类型 MUST 被拒绝；路径 MUST NOT 为绝对路径或含 `.` / `..` / 空段；同一路径 MUST NOT 重复或同时作为文件与目录。

#### Scenario: 源目录含符号链接
- **WHEN** `skill add` 的源目录包含符号链接
- **THEN** 拒绝并指出不支持的文件类型

#### Scenario: 不可信归档含越权路径
- **WHEN** 校验一个含 `../x` 或 `/etc/x` 条目的 tar
- **THEN** 校验失败

#### Scenario: frontmatter name 与 id 不符
- **WHEN** SKILL.md 的 name 不等于 id
- **THEN** 拒绝入库，vault 无任何残留

### Requirement: server 按 kind 的密文上限

server MUST 对 kind=`skill` 允许单条密文至 1.5 MiB；其它 kind MUST 仍限 512 KiB。

#### Scenario: 放宽仅限 skill
- **WHEN** 推送 600 KiB 的 skill 与 600 KiB 的 backup
- **THEN** skill 被接受，backup 被拒

### Requirement: rekey、doctor、orphan 覆盖 skills

`senv passwd`（rekey）MUST 重加密 `skills/` 下全部密文；`senv doctor` MUST 探测 `skills/` 密文可解密性并指认失败文件；`HasOrphanedData` MUST 在 metadata 缺失且 `skills/` 下存在 `.enc` 时返回 true。

#### Scenario: 丢失 metadata 与 index
- **WHEN** 数据目录仅剩 `skills/<g>/<id>.enc`，metadata 不存在，执行 init
- **THEN** 以 orphaned data 拒绝，不生成新 key

### Requirement: skill CLI

CLI SHALL 提供 `senv skill add|list|show|remove`。`show` MUST 只展示元数据与文件清单，不打印文件内容。本能力 MUST NOT 在家目录 skills 根写入任何内容。

#### Scenario: add 后 list/show
- **WHEN** `senv skill add demo --source <dir>` 成功后执行 `list` 与 `show demo`
- **THEN** `list` 含 demo 与描述，`show` 含文件清单，二者均不含文件正文
