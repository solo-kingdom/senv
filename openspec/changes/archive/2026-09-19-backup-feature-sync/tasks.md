## 1. syncschema 扩容（高优先级·安全）

- [x] 1.1 新增 `KindBackup`/`KindBackupMeta`；`ValidateIdentity` 与 text/text_meta 同构。验证：`go test ./internal/syncschema/ -race` 含合法/非法身份与既有 kind 不变
- [x] 1.2 更新依赖白名单计数的测试与文案（原「九 kind」类断言）。验证：相关测试全绿

## 2. 客户端通道

- [x] 2.1 `collectEntriesDiff` 遍历 `backups/`（组 meta + 条目）；`entryLocation` 映射回原路径。验证：`go test ./internal/provider/ -race` fixture 往返
- [x] 2.2 `internal/conflict`：backup 走 text 同构对比/合并，上限 `MaxBackupSize`。验证：`go test ./internal/conflict/ -race` 超限合并失败

## 3. server store 对齐

- [x] 3.1 store 校验测试：两新 kind 合法入库、非法身份整批拒绝。验证：`go test ./internal/server/store/ -race`

## 4. 文档与收尾

- [x] 4.1 skill 同步段写明 backup 随通道分发、server 须先升级。验证：与 `docs/senv-server.md` 发布顺序不矛盾
- [x] 4.2 `make check` 通过。验证：`make check`
