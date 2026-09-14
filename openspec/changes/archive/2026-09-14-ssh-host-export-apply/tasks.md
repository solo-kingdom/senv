## 1. 路径推导与渲染分层（design D1/D2/D7）

- [x] 1.1 新增未导出 `groupDir`（空组 → `_ungrouped`）与 `fragmentPath`/`materializePath`，替换 `MaterializePath` 全部调用点，渲染与落盘共用同一推导（验证：`grep -rn MaterializePath --include=*.go` 无旧扁平路径残留；`go build ./...` 通过）
- [x] 1.2 【安全·高优】`validateGroup` 增加 `/` 拒绝，host `validateAndSave` 路径挂载该校验（验证：`senv host add x --hostname h --group a/b` 与 `keypair import k --file f --group a/b` 均报错拒绝且零写入）
- [x] 1.3 实现 `Render(filter)` 纯渲染：解密被引用 keypair 取 group 推导 `IdentityFile`、跨组 ProxyJump 闭包并入发起组、真实组名撞 `_ungrouped` fail-fast，返回 map[组]片段/warnings/被引用集合，零文件副作用（验证：临时 HOME 下调用后 `~/.ssh/senv/` 无任何创建）
- [x] 1.4 1.1–1.3 配对单测：分组路径派生（含未分组）、`/` 拒绝、闭包并入、撞名报错、渲染零副作用（验证：`go test ./internal/ssh/ -race` 新增用例全绿）

## 2. 应用编排（design D4/D5）

- [x] 2.1 实现 `Apply(filter)`：Render → 写 `groups/*.conf`（0700/0600、整文件重渲染）→ 全量限定幽灵片段清理（验证：临时 HOME 全量导出两次，第二次组片段内容幂等；构造 vault 组改名后全量导出，旧组片段被删、单组导出不删其他组文件）
- [x] 2.2 【安全·高优】自动落盘编排：缺失补写（0700/0600）、已存在跳过不覆盖、keypair 不在本机 vault 只 warning；恒不提供覆盖开关（验证：预置同路径文件后导出，文件内容字节不变、摘要标 skipped）
- [x] 2.3 两阶段错误语义：渲染失败（proxyJump 悬空等）零文件副作用；写盘阶段局部失败尽力而为 + 汇总错误 + 非零退出（验证：构造悬空 proxyJump，断言临时 HOME 下无 groups/ 无落盘；构造单组写入错误，其余组完成且摘要含错误明细）
- [x] 2.4 2.1–2.3 配对单测（验证：`go test ./internal/ssh/ -race` 全绿）

## 3. Include 注册与撤回（design D3）

- [x] 3.1 【安全·高优】`internal/ssh/config_register.go`：`RegisterInclude`/`UnregisterInclude`，逐行精确匹配、顶部插入、`.senv-bak` 事务 + 原子 rename、保持原文件权限、幂等（验证：预置含手写内容的 `~/.ssh/config`，注册后顶部多一行且其余字节不变、`.senv-bak` 存在；重复注册 changed=false；撤回只删 senv 行）
- [x] 3.2 实现 `Unexport()`：`UnregisterInclude` + 删除 `groups/` 目录，`keys/` 与 vault 不动，幂等（验证：撤回后 groups/ 消失、keys/ 原样、重复执行报告无变更不报错）
- [x] 3.3 3.1–3.2 配对单测（验证：`go test ./internal/ssh/ -race` 全绿）

## 4. 未引用私钥清理（design D6）

- [x] 4.1 【安全·高优】实现 `Prune(force)`：遍历 `keys/<组>/`、与 host `identityKey` 引用集合比对（覆盖 keypair 改组后的旧路径文件）、列清单标注 keypair 是否仍在 vault、TTY 确认或 `--force`、未确认零删除、非 TTY 无 `--force` 报错（验证：构造引用/未引用/旧路径三文件，prune 只列未引用与旧路径，确认后只剩被引用文件）
- [x] 4.2 4.1 配对单测（验证：`go test ./internal/ssh/ -race` 全绿）

## 5. CLI 与审计（design D8）

- [x] 5.1 改造 `host export`：默认 Apply + 摘要输出（重建组/落盘/skipped/注册结果/warnings），`--output <path>|-` 纯渲染，新增 `--group`，`--host` 语义按 spec，保留 `--refresh`；写操作审计（验证：`go run . host export --help` 语法正常；临时 HOME 下默认导出直接可用 `ssh -G <别名>` 解析到 `IdentityFile`）
- [x] 5.2 新增 `host unexport` 命令 + 审计（验证：`go run . host unexport --help` 语法正常；导出→撤回→`ssh -G` 不再解析到 senv 片段）
- [x] 5.3 新增 `keypair prune` 命令（`--force`）+ 审计（验证：`go run . keypair prune --help` 语法正常）
- [x] 5.4 cmd 层配对测试：三命令参数校验、退出码、审计断言（验证：`go test ./cmd/ -race` 全绿）

## 6. TUI 适配（Q10 最小对齐）

- [x] 6.1 `internal/tui/ssh_tab.go` 预览调用改 `Render` 新签名，materialize 确认框与导出预览显示分组路径（`keys/<组>/<名>`）（验证：临时 HOME 起 TUI 单测渲染路径含 `keys/prod/`；`go build ./...`）
- [x] 6.2 6.1 配对测试：修正受签名影响既有用例（验证：`go test ./internal/tui/ -race` 全绿）

## 7. 文档与收尾

- [x] 7.1 按 AGENTS.md「同一变更」约束更新 `.agents/skills/senv-cli/SKILL.md`（export 应用模式、unexport、prune、分组布局、同步范围不变说明），改写 README/EXAMPLES 的 SSH 导出一节（含 BREAKING 提示与旧 `config.d` 教学移除）（验证：`go run . --help`、相关子命令 `--help`、README 示例命令全部可实际执行）
- [x] 7.2 ADR-0023 status `proposed` → `accepted`（验证：文档与实现行为一致抽查三项决策）
- [x] 7.3 全量回归（验证：`make check` 全绿；`go test ./... -race` 通过）
