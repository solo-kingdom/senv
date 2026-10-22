## Why

senv 需要把私有 Agent Skill（含 `SKILL.md` 的目录树）作为一等 vault 条目加密入库并经 `senv sync` 跨机同步。现有 `config` 只支持单文件，`backup` 非目录且 ≤512KiB，都不适合。本切片只做「入库 + 同步身份 + 基础设施」，本机应用（apply）留给下一片。

方案真源：`~/code/tasks/ongoing/senv-skills-config/design/skills-config.md` 与 `adr-skill-kind-encrypted-tree.md`。父 driver：`senv-skills-config-driver`。

## What Changes

- 新增同步 kind `skill`（grp=group，key=id）与 `skill_index`（grp/key 为空）。
- 存储：`skills/<group>/<id>.enc`（ustar tar 明文 ≤1 MiB，vault 密钥加密）与数据目录下明文 `skill_index.json`。
- server：`kind=skill` 单条密文上限 1.5 MiB，其它 kind 仍 512 KiB。
- rekey / doctor（`CheckConsistency`）/ `HasOrphanedData` / sync 收集（`entryLocation` + `collectEntriesDiff`）认识 `skills/` 与 skill 身份。
- 新增 `internal/skill`：Pack（目录→tar）、Inspect/Validate（白名单校验）、Manager（Add/List/Get/Show/Remove）。
- CLI：`senv skill add|list|show|remove`。**不含 apply。**
- 冲突渲染与 `senv history` 对两个新 kind 做安全展示（skill 正文只显示元数据/大小）。

## Non-goals

- `skill apply`、`.senv-owned`、双根落盘（`senv-skills-config-apply-e2e`）
- 文档与 senv-cli skill（`senv-skills-config-docs`）
- TUI/MCP 暴露、kiro、`unapply`、超限 pointer 模式

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| `.`（solo-kingdom/senv，worktree `worktrees/senv-skills-config`） | 必须 | syncschema / storage / provider / server store / conflict / cmd / 新包 skill |

## 验收标准

- [x] `skill`/`skill_index` 通过 syncschema 身份矩阵；server 与 client 共用
- [x] sync 收集与应用（`entryLocation`、`collectEntriesDiff`、pull apply）覆盖 skills 与索引，增量收集与全量一致
- [x] 明文 >1 MiB 被拒并点名上限；server 对 skill 放宽到 1.5 MiB，其它 kind 不变
- [x] tar 白名单（仅 TypeReg/RegA/Dir，拒绝绝对路径与 `..`）、要求 SKILL.md 且 frontmatter name == id、id 全局唯一
- [x] rekey、doctor、orphan 检测覆盖 skills，丢 index 后密文仍挡得住误 init
- [x] `senv skill add|list|show|remove` 可用，无 `apply`
- [x] 相关包 `go test` 通过

## 验证记录

见 `tasks.md`。
