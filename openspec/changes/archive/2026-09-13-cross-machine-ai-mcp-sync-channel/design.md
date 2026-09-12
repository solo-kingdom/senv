## Context

探索结论（2026-09-12，探查报告在 driver grill 会话）：

- `internal/syncschema/schema.go` 是 kind 白名单唯一真相源，server 端 `internal/server/store/store.go:342` 复用同一 `ValidateIdentity`，schema 扩容后 server 自动接受新 kind，零服务端改动
- pull 落盘唯一按 kind 分派的开关在 `localCache.entryLocation`（`internal/provider/server_state.go:134-152`），经 `tx.apply → AtomicWrite`（0600）；push 收集在 `collectEntriesDiff`（server_state.go:216-343），`add` 闭包签名 `add(kind, grp, key, ident, root, segments...)`
- LLM/MCP 档案本体已是 SSH-style 加密 blob：`storage.LLMProviderDirName = "llm_providers"` / `MCPServerDirName = "mcp_servers"`，文件名 `<alias>.enc`（`ConfigFileSuffix`），与 `consistency.go:208-230` 的扫描假设一致
- 冲突真相对照 grill D5 的措辞修正：实际机制是 server 端 revision 乐观锁（store.go:402）+ 客户端 `SyncConflictError` → 交互式解决器 / 非交互 `writeSyncConflictReport`（cmd/sync.go:180-201）。没有自动 LWW。"沿用现有机制"的实质是：不新增裁决逻辑，只在冲突报告与 audit 输出上对两个新 kind 追加对照 warning

## Goals / Non-Goals

- Goals：两个新 kind 端到端走通 push/pull（身份校验、收集、落盘、revision 跟踪）；冲突时新 kind 有 alias/revision 对照提示；新机器 bootstrap 默认拉取
- Non-Goals：凭据引用解析语义（cli 切片）、TUI 子视图（tui 切片）、ssh_host/ssh_keypair（D9 延后）、git remote provider 的 kind 扩容评估（走同一 syncschema，行为跟随，无需单独改动）

## Decisions

- D-a kind 常量与校验：`KindLLMProvider = "llm_provider"`、`KindMCPServer = "mcp_server"`；`ValidateIdentity` 分支为 grp 必须为空、key 走 `securefs.ValidateSegment`（alias 即文件名 stem）。理由：与 `config` 的 grp-empty 形态一致，alias 与磁盘名一一对应
- D-b 路径映射：`entryLocation` 两个新 case → `{cacheDataRoot, []string{storage.LLMProviderDirName|MCPServerDirName, key + storage.ConfigFileSuffix}}`。直接复用 `consistency.go` 对两个目录的既有命名假设，避免第二种文件命名
- D-c 收集：`collectEntriesDiff` 在 dataPath 顶层遍历段之后追加两个目录段：`ReadDir(LLMProviderDirName)` / `ReadDir(MCPServerDirName)`，仅取 `ConfigFileSuffix` 后缀文件，key = 去后缀名，`add(Kind..., "", key, ident, dataRoot, dir, file.Name)`。目录不存在时静默跳过（与 env/text 各组缺目录的处理一致）
- D-d 冲突对照 warning：`writeSyncConflictReport` 渲染时，conflict 条目 kind 属于 {llm_provider, mcp_server} 的，在条目块后追加一行 `⚠ 配置源 <kind> <alias> 双端均有修改：local rev N / remote rev M，请人工核对`；交互式解决器与 TUI 冲突模型不加新流程，仅依赖既有条目渲染（alias 已在 kind/key 字段可见）。audit 侧：`runServerSync` 冲突失败已有 `auditOp(op_sync, ..., "同步冲突 N 项")`，把含配置源冲突的事实并入 message（如 `同步冲突 N 项（含配置源 M 项）`）
- D-e bootstrap：无开关、无 opt-in——新 kind 条目走 `pullLocked` 既有 `applyRemoteOpts` 流程，天然随首次 pull 落地。唯一要验证的是 `--accept-remote` 重建后新 kind 文件也被清掉重建（走同一 removed/cleanup 路径）

## Risks / Trade-offs

- `collectEntriesDiff` 每次多扫两个目录（stat 级）；目录不存在时 ReadDir 一次 ENOENT，开销可忽略，不引入新的缓存
- server 端 push 批次校验接受新 kind 后，旧客户端收到含新 kind 的 pull 响应会因自身白名单拒绝 apply——多端混跑旧版本时新档案不落地（客户端 `validateRemoteEntries` 拒绝），不损坏本地状态；文档提示升级即可，不做版本协商
- 假冲突收养（`healFalseConflicts`）对新 kind 依赖同样的"密文一致"判定，密文格式无差别，无需特判

## Migration Plan

纯扩容，无数据迁移。既有 5 kind 行为不变（回归测试守住）；新 kind 首次 push/pull 即自适应。

## Open Questions

无（driver grill 已收敛）
