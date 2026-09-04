## 1. 存储层索引加载与分类

- [x] 1.1 【高优先级】重构 `normalizeConfigIndex`：先做结构性校验（穿越/不一致/绝对路径/卷标/NUL → 硬失败），结构通过后仅因 `:` 被拒的条目归入隔离集；新增 `LoadConfigIndexWithQuarantine` 返回 `(index, quarantined, err)`，`LoadConfigIndex` 委托并在隔离集非空时返回含修复指引的错误。验证：单测覆盖——冒号名进隔离集、`../` 名整体失败、key≠Name 整体失败、空 EncryptedFile 兼容不变
- [x] 1.2 【高优先级】`LoadConfigIndex`/`LoadConfigIndexWithQuarantine` 对 `config_index.json` 缺失（`errors.Is(err, os.ErrNotExist)`）返回空索引，其他读错误照常传播。验证：单测覆盖空库 list 返回空、create 首条成功、delete 报 not found
- [x] 1.3 补充 `internal/storage` 回归测试：混布索引（合法 + 冒号名 + 穿越名）下两类 API 的行为矩阵。验证：`go test ./internal/storage -run ConfigIndex -race` 通过

## 2. 只读路径警告透传

- [x] 2.1 为 config manager 增加带警告的 `ListWithWarnings`/`GroupsWithWarnings`（items + `QuarantineWarning{OldName, Hint}`），CLI `config list/groups` 打印 `⚠` 到 stderr 且退出码 0。验证：构造混布索引跑 `go test ./internal/config`，确认合法条目列出、警告含原名与 `senv config repair`
- [x] 2.2 TUI config tab 改用带警告加载，警告栏展示隔离条目与修复指引。验证：TUI 集成测试断言合法条目可见、警告文案出现
- [x] 2.3 `CheckConsistency` 遇隔离条目继续探测合法配置，隔离项计入警告不计入 failed/总数。验证：单测覆盖"合法+隔离+真实脱节"三态报告，doctor 不再因隔离中断

## 3. config repair 命令

- [x] 3.1 【高优先级】实现建议名生成与预检：`:`→`_`、结果过 `ValidateSegment`、与现有 key 及其他新名冲突即整体失败；`.enc` 缺失默认拒绝，`--drop-missing` 才允许丢弃。验证：单测覆盖改写、冲突、缺文件三分支
- [x] 3.2 【高优先级】实现修复执行：变更锁内先重命名 `.enc`（私有受控 rename，词法围栏 + 目标无 symlink）再 `AtomicWrite` 索引；`--dry-run`/`--yes`/交互确认三种模式。验证：单测断言修复后隔离集为空、文件与索引同步、中途失败不写索引
- [x] 3.3 接入 `cmd/config.go` 并输出修复计划表（旧名→新名）。验证：手跑 `senv config repair --dry-run` 与 `--yes`，确认示例中 `feg:ai-ops-portal.pub → feg_ai-ops-portal.pub`

## 4. 端到端验证

- [x] 4.1 全量回归：`make check`（fmt + vet + lint + test）通过；针对 b3b4c33 之前的存量索引格式做一次手工兼容演练（含冒号名修复、空库 list）。验证：输出无 "failed to load config index"，修复后 `config list`/`export` 正常
