## 1. syncschema 扩容

- [x] 1.1 `internal/syncschema/schema.go`：新增 `KindLLMProvider = "llm_provider"`、`KindMCPServer = "mcp_server"` 常量；`ValidateIdentity` 各加一个分支（grp 必须为空、key 走 `securefs.ValidateSegment`）
- [x] 1.2 `internal/syncschema` 单测：新 kind 合法身份通过；grp 非空 / key 为空 / 路径语义身份被拒；既有 5 kind 用例全部保持不变
- 验证：`go test ./internal/syncschema/ -race` 全绿

## 2. 客户端双向通道

- [x] 2.1 `internal/provider/server_state.go` `entryLocation`：加 `KindLLMProvider` / `KindMCPServer` 两个 case → `{cacheDataRoot, [LLMProviderDirName|MCPServerDirName, key+ConfigFileSuffix]}`
- [x] 2.2 `collectEntriesDiff`：追加 `llm_providers/`、`mcp_servers/` 两个遍历段（仅 `ConfigFileSuffix` 后缀，key=去后缀名，目录缺失静默跳过）
- [x] 2.3 单测：本地 fixture 放置两个目录的假 blob → 收集出现在待推送集合；pull 响应携带新 kind 条目 → 落盘到 `<dir>/<alias>.enc`、0600、revision 状态更新；增量收集（快照前后）行为与全量一致
- 验证：`go test ./internal/provider/ -race` 全绿

## 3. 冲突对照提示与 bootstrap

- [x] 3.1 `cmd/sync.go` `writeSyncConflictReport`：conflict 条目 kind 为两个新 kind 时，条目块追加"⚠ 配置源 <kind> <alias> 双端均有修改：local rev N / remote rev M"一行；`runServerSync` 的 audit message 在含配置源冲突时标注"（含配置源 M 项）"
- [x] 3.2 e2e（沿用 `server_registration_e2e_test` / provider 同步测试基建）：机器 A `ai provider add` + `mcp add` → push；机器 B（全新缓存目录）首次 pull → 两个目录档案落盘、`ai provider list` / `mcp list` 可见；B 本地改同一 alias → 冲突报告含对照行；`--accept-remote` 重建后新 kind 文件一并重建
- [x] 3.3 spec 场景逐条落测试（`specs/server-sync` delta：七 kind 接受 / bootstrap / 推送 / 冲突对照 / 加密落盘姿态）
- 验证：`go test ./cmd/... ./internal/provider/... -race` 全绿

## 4. 收尾

- [x] 4.1 `make check` 通过；driver proposal 验收标准中本切片对应项勾选（由 driver 轮次执行，不在本切片 tasks 内）
