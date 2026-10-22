## Why

`senv-skills-config-core` 让私有 skill 能加密入库并经 `senv sync` 跨机，但 B 机拿到密文后还不能用：没有把它写进 agent 读取的目录的命令。本切片补上 `senv skill apply`，并用一条 Go 判据测试证明整条链路（A 入库 → sync → A 消失 → B sync → apply）成立。

方案真源：`~/code/tasks/ongoing/senv-skills-config/design/skills-config.md` §2 与 §4。父 driver：`senv-skills-config-driver`。

## What Changes

- 新增 `senv skill apply [id | --group <g> | --all]`，支持 `--dry-run`、交互确认、`--yes`。目标家目录只用 `os.UserHomeDir()`，落到 `$HOME/.agents/skills/<id>` 与 `$HOME/.claude/skills/<id>`。
- 每个目标根的动作：`create` / `skip`（内容等价，比较排除 `.senv-owned`）/ `skip-conflict`（符号链接或无 `.senv-owned` 的已有目录，只警告）/ `backup+overwrite`（托管目录内容变化，旧树移到 skills 根之外）。
- 展开前对归档再次 `Validate`；只取 `Mode.Perm()`（丢弃 setuid/setgid/sticky，保留可执行位）；先写状态目录里的临时目录，再 rename 换入；成功后写空的 `.senv-owned`。
- 备份与临时目录放 `$HOME/.local/state/senv/skill-apply/`，不在任何 skills 根里。
- 新增判据 Go 测试 `TestSkillApplyAcrossMachinesE2E`，严格按设计 §4。

## Non-goals

- 文档与 `senv-cli` skill（`senv-skills-config-docs`）
- kiro 根、`skill unapply`、TUI/MCP 暴露
- 自动迁移存量软链；不调用或修改 dotf
- 备份目录的自动清理

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| `.`（solo-kingdom/senv，worktree `worktrees/senv-skills-config`） | 必须 | `internal/skill/apply.go`、`cmd/skill.go`、测试 |

## 验收标准

- [x] `senv skill apply` 支持 id / `--group` / `--all`，且恰好选一种
- [x] `--dry-run` 只出计划，不写任何文件（含状态目录）；无 `--yes` 时需确认，非交互 stdin 直接拒绝
- [x] 符号链接与无 `.senv-owned` 的目录 skip-conflict + warning，其它根继续，退出码为 0
- [x] 托管目录内容变化时 backup+overwrite，备份在 skills 根之外
- [x] 展开前 `Validate`；只取 Perm；临时目录 rename 换入；写 `.senv-owned`；坏归档不创建任何目录
- [x] 判据 Go 测试通过：独立 HOME_A/B、显式 git + 外部 bare remote、B 先有 vault 再 A sync、B sync 后才出现、RemoveAll(HOME_A) 后 apply、双根 `SKILL.md` name == id、真实 HOME 与真实 vault 无污染、未调用 dotf
- [x] `go build` / `go vet` / 相关 `go test` 通过；`openspec validate --strict` 通过

## 验证记录

见 `tasks.md` 第 4 节。
