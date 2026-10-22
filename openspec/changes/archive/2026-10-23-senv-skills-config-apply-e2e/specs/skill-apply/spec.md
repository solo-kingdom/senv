# Spec Delta

## ADDED Requirements

### Requirement: skill apply 目标与选择

`senv skill apply` SHALL 接受恰好一种选择器：skill id、`--group <group>` 或 `--all`。目标路径 MUST 由 `os.UserHomeDir()` 解析为 `$HOME/.agents/skills/<id>` 与 `$HOME/.claude/skills/<id>`，MUST NOT 使用 `--path` 或 `user.Current()`。命令 MUST NOT 调用 dotf。

#### Scenario: 选择器数量不对
- **WHEN** 不给选择器，或同时给 id 与 `--all`
- **THEN** 报错且不写任何文件

#### Scenario: 家目录随 HOME 变化
- **WHEN** 测试把 `HOME` 设为临时目录后执行 apply
- **THEN** skill 只写到该临时目录下的双根，真实家目录不变

### Requirement: apply 计划与确认

`apply` SHALL 先列出每个 skill 在每个根上的动作（create / skip / skip-conflict / backup+overwrite）。`--dry-run` MUST 只输出计划，不写 skills 根也不创建状态目录。有写入动作且未给 `--yes` 时 MUST 先确认；stdin 非交互且无 `--yes` MUST 拒绝执行。

#### Scenario: dry-run
- **WHEN** 运行 `senv skill apply demo --dry-run`
- **THEN** 输出含 `[create] demo`，两个根与状态目录都不存在

#### Scenario: 非交互无 --yes
- **WHEN** stdin 不是终端且未给 `--yes`
- **THEN** 错误信息提示 `--yes`，不写文件

### Requirement: 冲突与托管目录

目标是符号链接、非目录，或目录内没有普通文件 `.senv-owned` 时，`apply` MUST 跳过该根并输出 warning，MUST NOT 修改或替换它，其它根继续，命令不因此失败。带 `.senv-owned` 且内容（排除 `.senv-owned`）与归档一致时 MUST skip。带 `.senv-owned` 且内容不同时 MUST 先把旧树移到 skills 根之外的 `$HOME/.local/state/senv/skill-apply/backups/<id>.senv-backup-<时间>`，再换入新树。

#### Scenario: 软链与手写目录
- **WHEN** `.agents/skills/demo` 是符号链接，`.claude/skills/demo` 是无标记目录
- **THEN** 两处均不变，stderr 有两条 `warning: skipped`，退出码为 0

#### Scenario: 托管目录被改动
- **WHEN** 托管目录里的文件被本地修改后再次 apply
- **THEN** 目录恢复为归档内容，旧树完整保存在状态目录的 backups 下，skills 根里没有备份目录

### Requirement: 安全展开

`apply` 展开前 MUST 对归档再次执行 `Validate`；校验失败时 MUST NOT 创建任何 skill 目录。展开 MUST 只采用 `Mode.Perm()`（不得产生 setuid/setgid/sticky，不得有组/其他写位，属主始终可读写），保留可执行位；MUST 先写临时目录，再 rename 到目标；成功后目标下 MUST 存在空的 `.senv-owned`。归档含保留名 `.senv-owned` MUST 被拒绝。

#### Scenario: 恶意归档
- **WHEN** 归档含符号链接、`..` 路径、name 与 id 不符的 SKILL.md 或 `.senv-owned`
- **THEN** 报错，双根与状态目录均未被创建

#### Scenario: 特殊权限位
- **WHEN** 归档里文件模式为 `06777`
- **THEN** 展开后无 setuid/setgid，仍可执行，组/其他不可写

### Requirement: 跨机判据

在两台互相独立的机器（各自 HOME、配置目录、数据目录）与一个外部 bare git remote 下，A 机入库并 `senv sync` 后，即使 A 的 HOME 与源目录被删除，B 机在 `senv sync` 后执行 `skill apply --yes` MUST 在双根得到 `SKILL.md` 且其 frontmatter `name` 等于 id。

#### Scenario: 端到端
- **WHEN** B 先从 remote 取得 vault，A add 并 sync，B sync，删除 HOME_A 与源目录，B apply
- **THEN** B 的 `.agents/skills/<id>` 与 `.claude/skills/<id>` 各含 `SKILL.md`（name == id）、可执行脚本与 `.senv-owned`；真实家目录与真实 vault 中没有该 id；dotf 未被调用
