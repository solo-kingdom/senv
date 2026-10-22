## Why
senv 添加 skills 配置，用于私有 skill 同步。

完成判据：在独立临时 HOME_A 的数据目录登记一条合成样例私有 skill；经 senv sync（git 或本地/测试 server；不用共享线上 senv-server）到达独立临时 HOME_B 的数据目录；在移走或删除 HOME_A 数据目录与 A 侧 fixture 工作目录之后，仅用 B 侧数据目录完成本机应用，使 HOME_B 下同时存在 .agents/skills/<id>/SKILL.md 与 .claude/skills/<id>/SKILL.md，且 frontmatter name 与登记 id 一致；公开编目仍走 dotf agents -c，本任务不改其全量安装路径。

方案：`~/code/tasks/ongoing/senv-skills-config/design/skills-config.md`（ADR：`design/adr-skill-kind-encrypted-tree.md`）。批准：`~/code/tasks/ongoing/senv-skills-config/approve/APPROVED.md`。

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 不替代 `dotf agents -c` / 公开半公开编目全量安装
- 不做 `~/code` 项目级跨 agent skill 镜像
- 不迁移存量软链（agent-data / transit）进 vault
- v1 不做 kiro 根、TUI/MCP、超限 pointer 模式

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| .（solo-kingdom/senv） | 必须 | storage/syncschema/CLI/server 上限/rekey·doctor·orphan、senv-cli skill、README；planning root 本仓 openspec |
| ~/code（台账） | 参考 | tasks/ongoing/senv-skills-config；另镜像 `~/code/.agents/skills/senv-cli` |

## 验收标准
- [x] 在独立临时 HOME_A 的数据目录登记一条合成样例私有 skill；经 senv sync（git 或本地/测试 server；不用共享线上 senv-server）到达独立临时 HOME_B 的数据目录；在移走或删除 HOME_A 数据目录与 A 侧 fixture 工作目录之后，仅用 B 侧数据目录完成本机应用，使 HOME_B 下同时存在 .agents/skills/<id>/SKILL.md 与 .claude/skills/<id>/SKILL.md，且 frontmatter name 与登记 id 一致；公开编目仍走 dotf agents -c，本任务不改其全量安装路径

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `senv-skills-config-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 子 change 按依赖分批展开：依赖的实施项都已勾的切片才建子 change，起草时以 `worktrees/senv-skills-config` 里的代码为准
- 实现进度只认子 change 自己的 `tasks.md`；本文件的实施项只在对应子 change 全勾、`validate --strict` 通过、这一片已提交到任务分支后才勾
- 每勾掉一条实施项过一次片间检查点，不停人：按新认识补拆、合并或改顺序（只增删、改写未勾的实施项，记进 `TASK.md`「已做决定」），再展开依赖已满足的下一批；动到判据或范围的，按 taskrail 契约「意图变更」处理
- 两个根：planning root（`tasks/`、`openspec/changes/`）留在主工作树，只增量写本任务的目录，`openspec-*` 命令在这里跑；代码根是修改仓根目录下的 `worktrees/senv-skills-config`，读改代码、测试、回归、提交都在这里做，派给实现者的工作目录写它的绝对路径
- 涉及面里角色为 `必须` 的 git 修改仓一律建 worktree；修改仓不是 git 仓、或用户原话要求就地改时才在主工作树做，并记入 `TASK.md`「已做决定」。建法：先 fetch；基线默认 `origin/<默认分支>`，主工作树当前分支不是默认分支且领先于它时用 `HEAD`；执行 `git worktree add worktrees/senv-skills-config -b senv-skills-config <基线>`，分支已存在则 `git worktree add worktrees/senv-skills-config senv-skills-config`，worktree 已存在就复用；把 `worktrees/` 写进 `.git/info/exclude`；其他规则另有指定位置时以其为准。worktree 路径与基线记入「已做决定」。git 拒绝（如分支已在别处检出）时停下说明原因
- 主工作树里用户已有的改动和当前分支保持原样，不 stash / reset / 切分支。同一任务的 worker 共用这个 worktree，靠改动文件不重叠隔离
- 提交由编排者做，每片一次、收尾一次，只落在任务分支 `senv-skills-config`，不 push、不合进默认分支。收尾提交后保留 worktree，清理留给用户
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项；「需要用户决策」只停依赖该项的条目，其余条目继续
- 结束时在验证记录逐条列出未勾项与原因，不按 change 汇总；对用户按 user-report 四行说，不报编号

## 验证记录
- 2026-10-22：三片 core / apply-e2e / docs 全勾且 `openspec validate --strict` 通过；worktree 提交 `d9e0e02` → `eba0ab4` → `1da73be`；`go test ./... -count=1` 全绿；判据由 `cmd/skill_apply_e2e_test.go` 覆盖；`go run . skill --help` 可见 add/list/show/remove/apply；`~/code/.agents/skills/senv-cli` 已与仓内真源对齐。
