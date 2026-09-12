## 1. syncschema 扩容（高优先级·安全）

- [x] 1.1 `internal/syncschema/schema.go`：新增 `KindSSHHost = "ssh_host"`、`KindSSHKeypair = "ssh_keypair"`；`ValidateIdentity` 合并分支（grp 必须为空、key 走 `securefs.ValidateSegment`，与 `llm_provider`/`mcp_server` 同构）
- [x] 1.2 `internal/syncschema` 单测：两新 kind 合法身份（别名）通过；grp 非空 / key 为空 / 路径语义身份被拒；既有七 kind 用例全部不变
- 验证：`go test ./internal/syncschema/ -race` 全绿

## 2. 客户端双向通道（高优先级·安全）

- [x] 2.1 `internal/provider/server_state.go` `entryLocation`：加 `KindSSHHost`/`KindSSHKeypair` 两个 case → `{cacheDataRoot, [HostDirName|KeypairDirName, key+ConfigFileSuffix]}`
- [x] 2.2 `collectEntriesDiff`：追加 `hosts/`、`keypairs/` 两个遍历段（仅 `ConfigFileSuffix` 后缀、key=去后缀别名、目录缺失静默跳过），并入既有增量快照机制
- [x] 2.3 单测：fixture 放置两目录假 blob → 出现在待推送集合；pull 携带两新 kind → 落回 `<dir>/<alias>.enc`、0600、revision 状态更新；增量收集（快照前后）行为与全量一致
- 验证：`go test ./internal/provider/ -race` 全绿

## 3. 冲突呈现与 server store 对齐

- [x] 3.1 `cmd/sync.go` `isConfigSourceKind` 扩入两新 kind；确认 `internal/conflict/render.go` 不为两新 kind 增加解码分支（默认元数据渲染路径）
- [x] 3.2 单测：SSH 条目冲突报告含 alias+revision 对照提示行；渲染输出断言不含档案明文与 `KeyPairEntry.PrivateKey` 字符串
- [x] 3.3 `internal/server/store` 校验对齐测试：两新 kind 合法条目入库成功、非法身份整批拒绝
- 验证：`go test ./cmd/... ./internal/conflict/ ./internal/server/store/ -race` 全绿

## 4. spec 场景与收尾

- [x] 4.1 e2e（沿用 provider 同步测试基建）：机器 A `keypair import` + `host add`（引用该 keypair）→ push；机器 B 全新缓存首次 pull → 两目录档案落盘、`senv ssh host list` / `keypair list` 可见；B 本地改同一别名 → 冲突报告含对照行且无明文
- [x] 4.2 `make check` 通过；specs delta 场景逐条核对已落测试
- 验证：`make check` 全绿
