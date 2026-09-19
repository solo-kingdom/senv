## Why

独立 vault kind `backup` 的数据面与 CLI：不常用备份不能和 text 混用。本切片落地存储、Manager、`senv backup` 命令组、init/存量 `default`、512KB 与隐式建组闸门，以及与引用/根快捷的隔离。TUI/MCP/同步见后续切片。

编排见 `backup-feature-driver`。产品决策见 `tasks/archive/2026-09-18/backup-feature/`。

## What Changes

- 新增 `backups/{group}/{key}.enc` 存储与 `internal/backup` Manager（对照 `internal/text`）
- CLI `senv backup`：set/get/list/delete/import/export 与 group list/add/delete
- `senv init` 与存量 vault 幂等补建 backup `default`
- 常量 `MaxBackupSize`；禁止隐式建组；不提供 `-d/--decode`；根快捷仍写 text
- specs：新能力 `backup-storage`；delta `ref-system`、`group-key-shorthand`、`group-threshold`、`vault-description`
- `.agents/skills/senv-cli/SKILL.md` CLI 段

**安全性分析**：AES-256-GCM 与 text 同构；导出明文 0600、拒绝符号链接；list 不含 value。

## Non-goals

- TUI Backup Tab、全局搜索、MCP 工具（`backup-feature-surfaces`）
- server/git 同步 kind 白名单（`backup-feature-sync`）
- 引用、`{{backup:…}}`、改根快捷、TTL、CLI rename、覆盖确认

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 由 driver 准备段切分支 |

## 验收标准

- [ ] `senv backup` 完成 CRUD/import/export/group；`go run . backup --help` 语法正常
- [ ] value 明文 >512KB 拒绝；恰好 512KB 成功；缺失组拒绝且不建目录
- [ ] init 与已有 vault 均有 backup `default`；其它组须 `group add --description`
- [ ] `senv group:key` 仍写 text；`{{backup:…}}` 不被解析
- [ ] list 含 description、不含 value；skill CLI 段已更新
- [ ] `go test ./internal/backup/ ./internal/storage/ ./cmd/... -race` 覆盖超限与隐式建组
