# Tasks

## 1. 展开与冲突策略（`internal/skill/apply.go`）

- [x] 1.1 `Applier`：Plan / Apply，`prepare` 复用 `Validate` 并拒绝保留名 `.senv-owned`
- [x] 1.2 冲突判定：符号链接 / 非目录 / 无普通文件标记 → skip-conflict；等价 → skip（排除标记）；不同 → backup+overwrite
- [x] 1.3 展开：只取 Perm、临时目录 + rename、写 `.senv-owned`、备份放状态目录、第二步失败回滚旧树
- [x] 1.4 单测 `internal/skill/apply_test.go`：双根创建、软链与无标记目录、标记为软链、备份在根外、坏归档零副作用、Perm 收敛、托管树内软链

## 2. CLI（`cmd/skill.go`）

- [x] 2.1 `senv skill apply [id | --group | --all]`，`--dry-run` / `--yes` / `--refresh`，家目录只用 `os.UserHomeDir()`
- [x] 2.2 冲突 warning 写 stderr 且不致失败；单个 skill 失败不影响其它，最后返回非零；审计 `op_skill`
- [x] 2.3 CLI 测试 `cmd/skill_apply_test.go`：选择器、dry-run 零写入、确认与拒绝、冲突 warning、group/all/覆盖备份、未知 id 与空 vault

## 3. 判据 Go 测试（`cmd/skill_apply_e2e_test.go`，设计 §4）

- [x] 3.1 前置：配置/数据/`GetGitPath()` 均在临时 HOME 下，provider 显式 git，remote 为 HOME 之外的 bare，真实 vault 与真实 skills 根无该唯一 id
- [x] 3.2 A init + baseline sync；B 从 bare clone 得到 vault；A add + sync；B 在 sync 前没有 skill、sync 后才出现
- [x] 3.3 `RemoveAll(HOME_A)` 与 fixture 并断言已不存在；B `skill apply --yes`；双根 `SKILL.md` name == id、脚本可执行位、`.senv-owned`
- [x] 3.4 真实 HOME / 真实 vault 无污染；dotf PATH 陷阱未触发
- [x] 3.5 变异验证：注释掉 B 的第二次 sync，判据测试失败

## 4. 验证

- [x] 4.1 `go build ./... && go vet ./cmd ./internal/skill`
- [x] 4.2 `go test ./internal/skill ./cmd -run 'Skill|Doctor|History' -count=1`、全仓 `go test ./... -count=1`
- [x] 4.3 `openspec validate senv-skills-config-apply-e2e --strict`
