# server-migration Specification

## Purpose
定义 git 仓与 senv-server 之间双向迁移的行为：密文逐条搬运、不重新加密、目标端冲突时显式确认，迁移后数据可用性与一致性可验证。
## Requirements
### Requirement: 双向迁移

系统 SHALL 提供 `senv migrate to-server` 与 `senv migrate from-server` 两个方向，迁移内容 MUST 覆盖 metadata（salt/passwordKey/settings）与全部条目（env、text、config），vault 口令在迁移前后保持不变。

#### Scenario: git 迁到 server

- **WHEN** 用户在 git 模式数据仓执行 `senv migrate to-server`（目标 server vault 为空）
- **THEN** metadata 与全部条目密文写入 server，迁移后用原 vault 口令可在 server 模式解锁并读到一致数据

#### Scenario: server 迁回 git

- **WHEN** 用户执行 `senv migrate from-server`（目标 git 仓为空仓）
- **THEN** server 上的全部密文条目落回本地仓，可用原 vault 口令正常读写

### Requirement: 非空目标保护

目标端已存在数据时 MUST 停止并提示冲突，除非用户显式确认覆盖；MUST NOT 静默合并或部分写入。

#### Scenario: 目标非空

- **WHEN** 目标 server vault 已有条目，执行 `senv migrate to-server` 且未确认覆盖
- **THEN** 命令中止，目标数据不变，提示使用显式覆盖选项

### Requirement: 迁移过程零明文

迁移 MUST 以密文条目为单位搬运，MUST NOT 要求输入 vault 口令，MUST NOT 产生明文中间文件。

#### Scenario: 无需口令

- **WHEN** 执行任一方向迁移
- **THEN** 全程不提示输入 vault 口令，临时文件（如有）仅含密文

### Requirement: 迁移结果可核对

迁移完成后 SHALL 输出搬运条目的分类计数（env/text/config），失败时 MUST 指明失败条目与原因，并保证已写入部分可安全重试（幂等）。

#### Scenario: 中断重试

- **WHEN** 迁移中途网络失败，重新执行同一命令
- **THEN** 已完成条目不重复报错，迁移继续直至完成

