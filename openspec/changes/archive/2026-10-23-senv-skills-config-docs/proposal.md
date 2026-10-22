## Why

`senv skill add/list/show/remove/apply` 已实现（`senv-skills-config-core`、`senv-skills-config-apply-e2e`），但仓内 `senv-cli` skill 与 README 还没有说明。按仓库维护约定，用户可见命令变更必须同步文档；而且「先升级 server 再 `skill add`」「1 MiB / 1.5 MiB 上限」「不迁移存量软链」这些约束只写在方案里，agent 和用户读不到。

方案真源：`~/code/tasks/ongoing/senv-skills-config/design/skills-config.md` §5。父 driver：`senv-skills-config-driver`。

## What Changes

- `.agents/skills/senv-cli/SKILL.md` 新增「私有 Agent Skill」一节：五个子命令用法、与 dotf 边界、存量软链不自动迁移、明文 1 MiB / skill 密文 1.5 MiB、server provider 先升级 server 再 `skill add`/push、大批量可能触达 64 MB 请求体；速查与 server 同步段同步更新，版本 1.28 → 1.29。
- `README.md` / `README_EN.md`：功能列表加一条，新增「管理私有 Agent Skill」小节（原「Agent Skill」顺延为第 12 节）。
- `docs/senv-server.md`：发布顺序提示里把 `skill`/`skill_index` 列为同类新 kind。
- 把 `.agents/skills/senv-cli/` 整目录镜像到 `~/code/.agents/skills/senv-cli`（工作区副本，非软链）。

## Non-goals

- 不改任何 Go 代码、不改 `senv skill` 行为
- 不做 client 自动分批、`skill unapply`、kiro、TUI/MCP
- 不 push；不改线上 server

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| `.`（solo-kingdom/senv，worktree `worktrees/senv-skills-config`） | 必须 | `.agents/skills/senv-cli/`、`README.md`、`README_EN.md`、`docs/senv-server.md` |
| `~/code`（工作区） | 必须 | `.agents/skills/senv-cli/` 镜像副本 |

## 验收标准

- [x] SKILL.md 覆盖 add/list/show/remove/apply 用法、dotf 边界、软链不迁移、两个体积上限、server 先升级、64 MB 请求体提示
- [x] README 与 README_EN 都有 skills 小节与功能列表条目
- [x] 文档里的命令与 flag 和 `go run . skill <sub> --help` 一致
- [x] `openspec validate senv-skills-config-docs --strict` 通过
- [x] 工作区镜像与仓内 `.agents/skills/senv-cli/` 逐文件一致（`diff -r` 无差异），且不是软链
- [x] worktree 内已提交，未 push

## 验证记录

见 `tasks.md` 第 4 节。
