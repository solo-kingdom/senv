## 1. 去掉钥匙串后端 【高优】

- [x] 1.1 删除 `internal/session/keychain_store.go` 与 `keychain_store_test.go`；`defaultSessionStoreFor` 全平台返回 `tmpfsStore`；Load/Clear/ClearAll/legacy 收养不再引用钥匙串。验证：`rg -n 'keychainStore|/usr/bin/security' --glob '*.go'` 无生产命中；`go test ./internal/session -race` 编译失败处仅余过时断言。
- [x] 1.2 配对测试：改 `store_test.go` 等，Darwin 默认后端为 `tmpfsStore`；用 fake runner 断言 `session start` / `status` / `clear` / `clear --all` 零次 `security` 调用。验证：`go test ./internal/session ./cmd -race -count=1` 通过。

## 2. Darwin probe 与写路径回退 【高优】

- [x] 2.1 实现 `runtimefs_darwin.go`：`statfs` 的 `f_fstypename` 为 `tmpfs`/`ramfs` 则 memory，其余 unknown；statfs 失败当 unknown。验证：可注入 probe 的单测覆盖 tmpfs 放行、apfs/unknown 拒绝；`go test ./internal/session -race -run Filesystem` 通过。
- [x] 2.2 配对实现 `saveCache`：未开 `--insecure-cache` 时先 tmpfs；`ErrNoSecureSessionStore` 且 Darwin 则打印逃生舱警告并写 `diskCacheStore`；Linux 仍 fail closed。验证：Darwin/未证明 → 警告+0600 落盘且不写 `~/.local/share/senv/session/`；Darwin/已证明 tmpfs → 只写 runtime、不写 disk；Linux/未证明 → 非 0、不写盘。`go test ./internal/session -race` 通过。

## 3. 读路径与到期语义 【高优】

- [x] 3.1 确认读路径只合并 tmpfs + 磁盘逃生舱；双缓存仍 `errMultipleSessionCaches`。验证：单测覆盖只 disk、只 tmpfs、双缓存；无钥匙串分支。
- [x] 3.2 配对：磁盘逃生舱上 duration 不因 boot ID 变化失效（到 `expires_at`）；restart 仍因 boot ID 失效。验证：`go test ./internal/session -race -run 'Duration|Restart|Boot'` 通过。

## 4. CLI 文案与文档

- [x] 4.1 更新 `cmd/session.go` 帮助、`--insecure-cache` 说明与 `InsecureCacheWarning`：Darwin 无安全存储时已是默认写目标；Linux/CI 仍须显式 flag。验证：`go run . session start --help` 无 Keychain 表述；相关 `cmd` 测试通过。
- [x] 4.2 配对更新 README、SESSION_USAGE、SECURITY、RELEASE_NOTES、`.agents/skills/senv-cli/SKILL.md`：Unix 选型、stock Darwin 默认逃生舱、遗留钥匙串不读不删（可手动在钥匙串访问删除）。验证：`go run . --help` 与 `go run . session --help` 可运行；文档无「macOS 默认 Keychain」。

## 5. 回归

- [x] 5.1 `make check`；Linux 矩阵：tmpfs 正常、disk-backed XDG 拒绝、`--insecure-cache` 写/读/清。验证：命令输出与 spec 场景一致。
