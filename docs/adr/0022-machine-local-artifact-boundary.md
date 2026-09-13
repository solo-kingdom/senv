# 0022-machine-local-artifact-boundary

机器本地工件（machine-local artifact）统一登记在
`internal/storage/machine_local.go`：`tui-snapshot.enc`、`.senv-sync-state.json`、
`.senv-sync.lock`（dataPath 顶层），`server-token.json`、`mcp-exports.json`、
`agent-pointers.json`、`.senv-vault.lock`、`cache/`（configPath 顶层）。
server 同步收集、TUI 首屏指纹、rekey 预检、orphan/一致性检测与
git add/.gitignore 全部从该表派生，不再各自硬编码。

## 背景

TUI 首屏加密快照落在 `<dataPath>/tui-snapshot.enc`，而 server 同步的本地
收集规则是「dataPath 顶层任意 `.enc` 即 config 条目」。快照每次 TUI 退出/
写操作都用新 nonce 重写，密文 hash 必变，于是每次会话都产生待推送；当共享
vault 的另一台机器（或本机历史会话）推送了它自己的本地快照，就会撞服务端
乐观锁，报出 `config//tui-snapshot` 的假冲突（实测 local rev 1182 / remote
rev 1183，两端内容其实都是各自的本地缓存）。

同类问题不止一处，且机器本地文件的清单此前散落在四个地方：

- `storage/server_token.go` 的 `gitIgnoredMachineLocal`（写 `.gitignore`）
- `git/manager.go` 的 `machineLocalExcludePathspecs`（`git add` 兜底排除）
- `provider/server_state.go` 硬编码跳过同步 state
- `tui/snapshot_cache.go` 硬编码跳过快照与锁

而 `rekey`/`orphan` 检测完全没有感知：rekey 会把快照当「未索引 config 密文」
直接失败关闭，`senv init` 会把只剩快照/状态的目录误判为 orphan 而拒绝，
git 模式还会把快照与 `agent-pointers.json`（ADR-0003 已声明机器本地）提交推送。

## 决策

1. **单一登记表**：`internal/storage/machine_local.go` 持有
   `MachineLocalArtifact{Name, Scope, IsDir}` 清单与查询函数
   （`IsMachineLocalDataArtifact` / `IsMachineLocalConfigArtifact` /
   `MachineLocalGitIgnoreEntries` / `MachineLocalGitExcludeGlobs`）。新增
   机器本地文件必须在此登记，并有单元测试锁定集合防漂移。
2. **所有扫描点查询登记表**：同步 collect、TUI 指纹、rekey 预检遍历、
   `HasOrphanedData`、generated `.gitignore` 与 `git add` pathspec 全部删除
   各自的私有列表。
3. **不迁移文件位置、不改加密格式**：快照仍在 `<dataPath>/tui-snapshot.enc`，
   只在其参与的扫描点排除；回退旧版本仍可读写该文件。
4. **远端遗留用 tombstone 幂等清理**：排除采集后 `collectDirty` 会把 state
   里残留的 `config//tui-snapshot` 判为删除，经既有
   `pushLocked(removedEntries)` 路径清理，无需服务端改动或一次性命令。
5. **rekey 例外只作用于预检遍历**：`classifyRekeyEntry` 对 journal/控制文件
   仍 fail closed（见 rekey-recovery spec），机器本地跳过在遍历回调里、且在
   `ValidateSegment`/symlink 检查之后，不放松路径安全。

## 残余

- 仍在运行的旧客户端会继续推它自己的本地缓存。在旧客户端全部升级前，远端
  可能反复出现该条目；新客户端每次 push 都会再次 tombstone 清理，且不会把
  它写回本地。协议无变化，新旧可混跑，无需强制升级窗口。
- `.gitignore` 新增条目为 `cache/`（目录级），可能过度匹配同名目录；config
  密文均为 `.enc` 顶层或受管子目录，不受影响。

## 相关

- ADR-0003：`agent-pointers.json` 为本机状态，不进 vault、不随同步分发。
- ADR-0021：`server-token.json` 的「文件隔离 + `.gitignore` + `git add`
  排除」三层防线，本 ADR 把该先例推广为登记表。
