## 1. 机器本地工件登记表

- [x] 1.1 新增 `internal/storage/machine_local.go`：定义 `MachineLocalScope`、`MachineLocalArtifact` 与上表 8 条登记，实现 `IsMachineLocalDataArtifact` / `IsMachineLocalConfigArtifact` / `MachineLocalGitIgnoreEntries` / `MachineLocalGitExcludeGlobs`；文件头注明“新增机器本地文件必须在此登记”。验证：`go build ./...` 通过。
- [x] 1.2 为登记表补单元测试 `internal/storage/machine_local_test.go`：锁定登记集合（防漂移）、精确匹配（`tui-snapshot.encX`、子路径 `/tui-snapshot.enc` 不匹配）、scope 隔离、gitignore/pathspec 派生输出。验证：`go test ./internal/storage/ -run MachineLocal -v` 通过。

## 2. server 同步排除与远端清理 【高优先级｜安全】

- [x] 2.1 修改 `internal/provider/server_state.go` 的 `collectEntriesDiff`：顶层扫描用 `IsMachineLocalDataArtifact(name)` 替换硬编码 `syncStateFileName` 判断，并保留 env 遗留前缀逻辑。验证：`go test ./internal/provider/ -run Collect -v` 通过。
- [x] 2.2 补回归测试：顶层放置 `tui-snapshot.enc` 与 `.senv-sync-state.json` 时收集结果不含对应条目，且其内容变化不改动 dirty 计数。验证：新增测试跑通且失败于修改前代码。
- [x] 2.3 补 tombstone 清理测试：state 中预置 `config//tui-snapshot`、本地不采集时，一次 push 生成删除标记、state 条目移除、`validateNoStateRegression` 不报错。验证：`go test ./internal/provider/ -run 'Conflict|Tombstone|Dirty' -v` 通过。

## 3. TUI 快照指纹

- [x] 3.1 修改 `internal/tui/snapshot_cache.go` 的 `walkManifest`：顶层排除改为查询 `IsMachineLocalDataArtifact`，删除包内 `snapshotCacheFileName`/`syncLockFileName` 的 bespoke 排除逻辑（常量本身保留用于读写路径）。验证：`go test ./internal/tui/ -run Snapshot -v` 通过。
- [x] 3.2 补测试：新增一个任意机器本地文件（如 `.senv-sync-state.json`）后指纹不变；`tui-snapshot.enc` 自身变化不改变指纹。验证：新增测试通过。

## 4. rekey 预检 【高优先级｜安全】

- [x] 4.1 修改 `internal/storage/rekey.go`：在遍历回调中、通过 `ValidateSegment` 与 symlink 检查之后，对顶层 `IsMachineLocalDataArtifact` 名字直接跳过；其余顶层未索引 `.enc` 仍保持失败关闭。验证：`go test ./internal/storage/ -run Rekey -v` 通过。
- [x] 4.2 补测试两个方向：存在 `tui-snapshot.enc` 时 rekey 预检通过且文件未被改动；存在真正未索引顶层密文时仍在写入前失败。验证：新增测试通过，且第二个用例在实现前已通过（防止放松失败关闭）。

## 5. orphan 与一致性探针

- [x] 5.1 修改 `internal/storage/consistency.go` 的 `HasOrphanedData`：顶层跳过机器本地工件；`metadata.json` 存在时行为不变。验证：`go test ./internal/storage/ -run 'Orphan|Consistency' -v` 通过。
- [x] 5.2 补测试：仅有 `tui-snapshot.enc`/`.senv-sync-state.json` 而无 metadata 时 `Initialize` 不再返回 `ErrOrphanedData`；真实受管密文仍触发拒绝（`cmd/init` 路径可覆盖）。验证：`go test ./internal/storage/ ./cmd/ -run 'Orphan|Init' -v` 通过。

## 6. git add 与 .gitignore 【高优先级｜安全】

- [x] 6.1 改造 `internal/storage/server_token.go` 的 `EnsureGitIgnoreServerToken`：从 `MachineLocalGitIgnoreEntries()` 生成条目，保留函数名做薄包装；`Initialize` 调用点不变。验证：`go test ./internal/storage/ -run GitIgnore -v` 通过。
- [x] 6.2 修改 `internal/git/manager.go`：`machineLocalExcludePathspecs` 改为从 `storage.MachineLocalGitExcludeGlobs()` 派生（按需引入 import，确认无循环依赖）。验证：`go test ./internal/git/ -run AddExclude -v` 通过。
- [x] 6.3 补测试：工作区含 `tui-snapshot.enc`、`agent-pointers.json`、`.senv-sync-state.json`、`.senv-sync.lock` 时 `git add` 后均不在暂存区；`.gitignore` 补写幂等且保留既有内容。验证：新增/扩展 `add_exclude_test.go`、`server_token_test.go` 通过。

## 7. 端到端验证与文档

- [x] 7.1 在真实 server vault 上执行 `senv sync`，确认远端 `config//tui-snapshot` 被删除、`tui-snapshot.enc` 本地仍存在、后续 TUI 会话不再产生该条目（用 `senv sync` 输出与 sync-state 核对）。验证：`senv sync` 无冲突、state 中不再有 `config//tui-snapshot`。
- [x] 7.2 新增 `docs/adr/0022-machine-local-artifact-boundary.md`：记录登记表决策、D1–D5、旧客户端残余。验证：文件存在且被 `docs/` 索引/链接引用（如有索引）。
- [x] 7.3 全量门禁：`make check`（fmt + vet + lint + test）通过；确认未修改 server 协议、sync schema、CLI 参数。验证：`make check` 零失败。
