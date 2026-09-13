## Context

机器本地工件（machine-local artifact）是指在单机才有意义、不得进入同步通道或版本库的文件。当前它们的处理散落在至少五个位置，且没有唯一事实来源：

- `internal/storage/server_token.go`: `gitIgnoredMachineLocal = [server-token.json, mcp-exports.json]`，用于生成 `.gitignore`。
- `internal/git/manager.go`: `machineLocalExcludePathspecs` 重复同样两个名字，作为 `git add` 的防御性排除。
- `internal/provider/server_state.go`: 收集时硬编码跳过 `.senv-sync-state.json`；`.senv-sync.lock` 因不是 `.enc` 而“碰巧”被跳过。
- `internal/tui/snapshot_cache.go`: 指纹遍历硬编码排除 `tui-snapshot.enc` 与 `.senv-sync.lock`。
- `internal/storage/rekey.go` / `consistency.go`: 无感知——rekey 把顶层任意 `.enc` 当受管 config，orphan 检测把任意顶层 `.enc` 当用户密文。

后果（见 proposal.md - Why）：`tui-snapshot.enc` 被 server 同步收成 `config//tui-snapshot` 并产生假冲突；`senv rekey` 因 `unindexed encrypted file` 失败；`senv init` 可能误报 orphan；git 模式会提交快照与 `agent-pointers.json`。ADR-0003 已声明 `agent-pointers.json` 为机器本地，ADR-0021 已为 `server-token.json` 建立“文件隔离 + .gitignore + git add 排除”的三层先例；本变更把该先例推广为一个登记表。

## Goals / Non-Goals

**Goals:**
- 建立唯一的机器本地工件登记表，并让所有扫描点从它派生行为。
- 修复 `tui-snapshot.enc` 被同步/误判/提交的全部路径。
- 让同类工件（同步 state、锁、agent 指针、模型目录缓存）获得一致边界。
- 幂等清理远端已存在的误收条目。

**Non-Goals:**
- 不把 config 采集改成 config index 驱动（当前 server index 为空但有 36 个 config 密文，index 驱动会造成删除误判）。
- 不迁移快照/state 的文件位置，不改加密格式或权限。
- 不改 server 协议、sync schema、CLI 参数。

## Decisions

### D1. 登记表放在 `internal/storage`，带 scope

新增 `internal/storage/machine_local.go`：

```go
type MachineLocalScope int
const (
    ScopeDataPath MachineLocalScope = iota // 位于 dataPath（vault 数据树）
    ScopeConfigPath                        // 位于 configPath（仓库根）
)

type MachineLocalArtifact struct {
    Name  string            // 单路径段文件名或目录名
    Scope MachineLocalScope
    IsDir bool
}
```

登记内容（本轮全量）：

| Name | Scope | IsDir | 说明 |
|------|-------|-------|------|
| `tui-snapshot.enc` | dataPath | 否 | TUI 首屏快照（加密缓存） |
| `.senv-sync-state.json` | dataPath | 否 | server 同步状态 |
| `.senv-sync.lock` | dataPath | 否 | 同步进程锁 |
| `server-token.json` | configPath | 否 | server provider token |
| `mcp-exports.json` | configPath | 否 | MCP 导出账本 |
| `agent-pointers.json` | configPath | 否 | 本机 agent 指向（ADR-0003） |
| `.senv-vault.lock` | configPath | 否 | vault 变更锁 |
| `cache` | configPath | 是 | 机器本地缓存目录（模型目录等） |

导出查询函数（按 scope 精确匹配顶层单路径段）：

```go
func IsMachineLocalDataArtifact(name string) bool
func IsMachineLocalConfigArtifact(name string) bool
func MachineLocalGitIgnoreEntries() []string   // 全部 Name（IsDir 追加 "/"）
func MachineLocalGitExcludeGlobs() []string     // git pathspec :(exclude,glob)**/<name>
```

**替代方案**：把登记表放在新 leaf 包 `internal/machineLocal` 供 storage/git 共用。放弃原因：storage 已是 git 与 provider 的共同下游，新增包只增一层转发；`git` 目前不 import storage，新增该依赖无环（storage 不 import git）。若后续出现依赖环，再抽出 leaf 包。

**替代方案**：把快照移到 `$XDG_CACHE_HOME/senv/` 彻底移出仓库。放弃原因：会话缓存的 tmpfs/权限模型不适用；且无论放哪，git 模式的仓库根都可能覆盖，仍需要登记表，迁移只增加一次性迁移成本。

### D2. 所有扫描点查询登记表，删除重复列表

| 扫描点 | 现状 | 改为 |
|--------|------|------|
| `provider/server_state.go` collect | 硬编码 `syncStateFileName` | `IsMachineLocalDataArtifact(name)` 跳过 |
| `tui/snapshot_cache.go` walkManifest | 硬编码 snapshot+lock | `IsMachineLocalDataArtifact(e.Name)` 跳过 |
| `storage/rekey.go` classifyRekeyEntry/遍历 | 报 `unindexed encrypted file` | 顶层 `IsMachineLocalDataArtifact` 先跳过，不进入分类 |
| `storage/consistency.go` HasOrphanedData | 任意顶层 `.enc` 当用户密文 | 跳过机器本地工件 |
| `storage/server_token.go` EnsureGitIgnoreServerToken | 私有两元素列表 | 从 `MachineLocalGitIgnoreEntries()` 生成（保留旧函数名做薄包装） |
| `internal/git/manager.go` | 私有 pathspec 列表 | 从 `MachineLocalGitExcludeGlobs()` 派生 |

**替代方案**：只修 `tui-snapshot.enc` 一处。放弃原因：同类问题会以“下一个本地缓存文件”的形式复发；登记表把“新增本地工件必须显式登记”变成默认动作。

### D3. 保持快照路径与格式不变，靠排除而非搬迁

快照仍写 `<dataPath>/tui-snapshot.enc`，加密姿态、版本号、指纹逻辑均不变，只在其参与的扫描点排除。这样无需数据迁移，回退旧版本也安全（旧版本仍能读写该文件，只是又会误同步，见 Risks）。

### D4. 远端遗留条目用 tombstone 幂等清理

排除采集后，`collectDirty` 会把 state 中仍存在、本地已不采集的 `config//tui-snapshot` 判为删除，生成 tombstone；`pushLocked` 已把删除 id 传给 `saveStateOpts.removedEntries`，可合法通过 `validateNoStateRegression`。无需服务端改动，也无需一次性“清理命令”；每台机器首次同步各自清理一次，幂等。

### D5. 向后兼容与旧客户端残余

新客户端不会再把机器本地工件入同步集合；仍在运行的旧客户端会继续推它自己的本地缓存。这是**残余**：在旧客户端全部升级前，远端可能反复出现该条目，但不会污染新客户端的本地状态（新客户端只会在下次 push 用 tombstone 再次清理）。协议无变化，因此新旧可混跑；不需要强制升级窗口。

## Data Flow

```
                 internal/storage/machine_local.go
                    (唯一登记表 + 查询函数)
                              │
   ┌──────────────┬───────────┼──────────────┬───────────────┐
   ▼              ▼           ▼              ▼               ▼
server collect  tui 指纹   rekey 遍历   orphan 探针   git add / .gitignore
 (跳过)         (跳过)      (跳过)       (跳过)        (排除路径 + 忽略行)
   │
   ▼
collectDirty: state 有、current 无 → tombstone
   │
   ▼
pushLocked(removedEntries) → server 删除误收条目（幂等）
```

## Error Handling

- **登记表查询**：纯字符串匹配，无 I/O、不返回错误；调用方无需新增错误路径。
- **collect / 指纹**：排除是“更少文件”，不影响既有 fail-closed 语义（目录遍历/I・O 错误仍返回错误）。
- **rekey**：只跳过登记在案的顶层名字；对其它顶层 `.enc` 仍保持“未索引即失败关闭”。文件系统遍历错误（权限/I/O）MUST 继续中止 rekey。跳过发生在 `ValidateSegment`/symlink 检查之后，不放松路径安全。
- **orphan**：跳过仅限精确名字匹配，不引入通配；`metadata.json` 存在时行为不变。
- **git**：`.gitignore` 补写失败按既有 `Initialize`/`SaveServerToken` 路径返回错误；git add 排除是 defense-in-depth，`.gitignore` 缺失也不暂存。

## Risks / Trade-offs

- [旧客户端继续推快照] → 新客户端 tombstone 幂等清理；文档标注残余与升级收益。vault 假冲突在升级后消失，但旧客户端之间仍可能互相冲突（不可避免）。
- [登记表与真实文件漂移（新增本地文件忘记登记）] → 在登记表文件头写明“新增机器本地文件必须在此登记”；用测试锁定登记集合（见 tasks）。
- [gitignore 新增 `cache/` 可能过度匹配] → config 密文均为 `.enc` 顶层或受管子目录，不会被 `cache/` 命中；如需更精确可改为 `cache/models-dev.json`，但会漏掉后续缓存文件，暂取目录级。
- [远端 `config//tui-snapshot` 若被某用户当作真实 config] → 现实约束：config 的权威定义是 config index，而该条目不在任何 index 中，且名字不属于用户配置命名惯例；tombstone 只删该名字。设计上接受这个极小概率。

## Migration Plan

1. 先落登记表与其单元测试（不改行为）。
2. 接入 collect/指纹/rekey/orphan/git 五个消费点，各自补回归测试。
3. 在真实 server vault 上跑一次 `senv sync`，确认 `config//tui-snapshot` 被 tombstone 清理且 `tui-snapshot.enc` 本地保留。
4. 补 ADR-0022（机器本地工件边界），并在 `docs/senv-server.md` 或 README 标注旧客户端残余。
5. 回滚策略：各消费点独立，可按提交粒度回滚；登记表本身无状态、无破坏性。

## Open Questions

- 是否把 `settings.json` 里的非敏感字段也纳入登记表？暂不：settings 是有意同步的 vault 配置，不属于机器本地工件。可在实现后按需追加一条 ADR。
