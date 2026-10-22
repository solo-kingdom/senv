## Context

冻结方案见台账 `~/code/tasks/ongoing/senv-skills-config/design/skills-config.md` §2、§4。core 已提供 `internal/skill`（Pack / Inspect / Validate / Manager.Archive）与 CLI add/list/show/remove。本片只加「应用到本机」和判据测试。

## Decisions

1. **Applier 独立于 Manager**：`internal/skill/apply.go` 的 `Applier{Home, Roots, StateDir}` 只吃 tar 字节，不碰 vault；CLI 负责取归档、选目标、确认。这样冲突策略可以脱离加密存储单测。
2. **Plan 与 Apply 都从 `prepare` 开始**：`prepare` 调 `Validate`，再把归档读入内存树（≤1 MiB），并拒绝保留名 `.senv-owned`。校验失败时不创建任何目录或状态目录。`Apply` 对每个根在写入前重新规划，避免确认到执行之间目标变化。
3. **所有权判据**：目标是符号链接、不是目录、或没有「普通文件」`.senv-owned` → `skip-conflict`。标记本身是符号链接不算托管。
4. **内容等价**：按相对路径比文件内容、是否可执行、目录集合；排除根下 `.senv-owned`；托管树里出现符号链接或特殊文件视为有差异（会 backup+overwrite，把它带走）。
5. **权限**：只取 `hdr.Mode` 的 `Perm()`，文件 `(perm|0600)&^022`，目录 `(perm|0700)&^022`。丢掉 setuid/setgid/sticky 和组/其他写位，保证属主始终能读写，后续比较和清理不会卡住。
6. **原子换入**：临时目录建在 `StateDir/tmp`（skills 根之外），展开完成并写入 `.senv-owned` 后 `os.Rename` 到目标。覆盖时先把旧树 rename 到 `StateDir/backups/<id>.senv-backup-<UTC 时间>`，再 rename 新树；第二步失败则把旧树 rename 回去。
7. **状态目录**：固定 `$HOME/.local/state/senv/skill-apply`，不读 `XDG_STATE_HOME`，行为可预测、测试可沙箱。要求与 skills 根同一文件系统；rename 失败时错误里点明这一点，不静默退化成复制。
8. **CLI**：`apply` 要求恰好一个选择器。冲突只写 stderr warning，不影响退出码；单个 skill 校验或写入失败则继续处理其它 skill，最后返回非零。`--dry-run` 不提示确认。非交互 stdin 且无 `--yes` 拒绝执行。沿用 `list/show` 的 `autoPull`，server provider 下先拉取。
9. **判据测试**：放在 `cmd` 包（需要 `configPathFn`/`dataPath` 注入与 `syncCmd`/`skill*Cmd`）。HOME_A、HOME_B 与 bare remote、fixture 各自是独立 `t.TempDir()`。git 通过 `GIT_CONFIG_GLOBAL=/dev/null`、固定作者身份与临时 `XDG_CONFIG_HOME` 与真实配置隔离。dotf 用 PATH 陷阱（排在 PATH 最前的假 `dotf`，被调用就写标记文件）。

## Risks / Trade-offs

- 两次 rename 之间有极短窗口目标目录不存在；崩溃时旧树在备份目录里，不会丢。Linux `renameat2(RENAME_EXCHANGE)` 可消除窗口，但不跨平台，v1 不用。
- 状态目录与 skills 根不在同一文件系统时 apply 失败（例如 `~/.claude` 是指向别的盘的软链）。选择报错而非复制，换取原子性。
- 备份不自动清理，由用户手动删。
- 崩溃遗留的 `StateDir/tmp/*` 不自动回收。

## Migration Plan

纯新增命令。回滚：删除 `apply` 子命令；已写入的目录保留（带 `.senv-owned`，可手动删除）。
