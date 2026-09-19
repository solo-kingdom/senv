## Why

新增独立 vault kind `backup`：备份不常用数据，不与常用 text 混在同一体验里。

已冻结方案（探索任务 `backup-feature`）：字段/加密/命令/MCP/分组规则对齐 text；value 明文上限 512KB、超限拒绝；独立 TUI Backup Tab；`senv init` 预置 backup `default`；无 `{{backup:…}}`、不抢根快捷；随 git/server 同步；更新 senv-cli skill 并补 `backup-storage` spec。

产品决策见归档探索任务：`tasks/archive/2026-09-18/backup-feature/`（`TASK.md`、`design/adr-separate-kind.md`、`design/adr-v1-scope.md`）。

实现对照 text：`cmd/text.go`、`internal/text/manager.go`、`internal/tui/text_tab.go`、`internal/syncschema/schema.go`、`openspec/specs/text-storage/spec.md`。

## What Changes

- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals

- TTL / 本机缓存 / 自动驱逐
- `{{backup:…}}` 以及 backup 内容内的 env/text 引用解析
- 改根快捷 `senv <group:key>`（仍只写 text）
- 从 text 自动搬家、目录级备份、超过 512KB
- CLI rename（改名只走 TUI，与 text 相同）
- 覆盖确认（`set`/`import` 仍 upsert）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准

- [x] CLI `senv backup` 具备 set/get/list/delete/import/export 与 group list/add/delete；根快捷仍只写 text
- [x] TUI 独立 Backup Tab，布局/键位对齐 Text；全局搜索 `S` 含 backup 的 group/key/description，不搜正文
- [x] MCP：`list` 无 value，`get/set/delete` 有正文；分组 `kind=backup`；无保留组封锁
- [x] `senv init` 预置 backup `default`；禁止隐式建组；TUI 中 `default` 不可改名/删除；description 最多 2048 字节
- [x] 单条 value 明文 512KB 超限拒绝；`set`/`import` upsert 无确认
- [x] git/server 同步覆盖新 kind；不参与 `{{…}}` 引用
- [x] `.agents/skills/senv-cli/SKILL.md` 已更新；交付 `backup-storage` spec；超限与隐式建组有测试

## Driver 协议

- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `backup-feature-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/backup-feature-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-18 propose：拆 3 个子 change（core / surfaces / sync）；driver `skip_specs: true`；`openspec validate --strict --type change backup-feature-driver` 通过。
- 2026-09-18 apply：core / surfaces / sync 三个子 change tasks 全勾且 `openspec validate --strict --type change` 均通过。回归：`gofmt`/`go vet`/`golangci-lint run --new-from-rev=origin/main` 新代码 0 issue；`go test -race ./...` 全绿。整仓 `make check` 的 lint 仍会被 origin/main 基线告警挡住（与 core 4.2 相同），未改无关文件。`go run . mcp list-tools` 共 25 个工具，含 `senv_backup_get/set/delete/list`。
- 2026-09-19 archive：主 spec 已合入（新增 `backup-storage`；更新 `ref-system`/`group-key-shorthand`/`vault-description`/`group-threshold`/`tui-viewer`/`server-sync`）。子 change 归档为 `openspec/changes/archive/2026-09-19-backup-feature-{core,surfaces,sync}`。driver 仍待 3.3 提交。
- 2026-09-19 收尾：勾选 3.3，交付改动（backup 功能代码 + skill + spec + 归档件）在 `backup-feature` 分支单笔提交；driver 归档至 `openspec/changes/archive/2026-09-19-backup-feature-driver`。
