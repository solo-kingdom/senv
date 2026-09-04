## 1. Provider rollback diagnostics

- [x] 1.1 **[高优先级][实现]** 重构 `internal/provider` cache transaction rollback，使父目录准备、restore write、remove 与 root 获取的每个错误均累积并与前向 apply 错误关联；确保失败路径不保存 state/revision。验证：运行 `go test ./internal/provider -run '^$'`。
- [x] 1.2 **[高优先级][测试]** 为 forward apply 失败后 restore atomic write 失败、父目录准备失败和多个 rollback 错误增加故障注入测试，断言错误包含 rollback failure 且 state 未推进。验证：运行 `go test ./internal/provider -run 'Test.*(Apply|State|Rollback).*Failure' -count=1`。

## 2. Session 与 vault generation 线性化

- [x] 2.1 **[高优先级][实现]** 将 `session.StartSession` 的口令验证、metadata/KDF 读取、key 验证和 cache 提交置于同一个 vault mutation lock lease，保证失败不提交 cache。验证：运行 `go test ./internal/session ./internal/storage -run '^$'`。
- [x] 2.2 **[高优先级][测试]** 增加可控屏障，覆盖 rekey 在 start 前/后竞争、旧口令与新口令结果以及 cache 覆盖，断言成功 start 的 cache 可立即验证。验证：运行 `go test -race ./internal/session ./internal/storage -run 'Test(StartSession|Rekey).*Concurrent' -count=1`。
- [x] 2.3 **[高优先级][实现]** 在可信 fallback runtime root 中实现每用户 no-follow 生命周期锁，串行化 fallback cache 创建、提交、枚举与过期目录清理。验证：运行 `go test ./internal/session -run '^$'`。
- [x] 2.4 **[高优先级][测试]** 以两个并发 fallback start、交错 cleanup 与 lock/symlink/ownership 失败夹具验证最终恰有可验证 cache 或明确错误，绝不双成功零 cache。验证：运行 `go test -race ./internal/session -run 'Test.*Fallback.*(Concurrent|Cleanup|Lock)' -count=1`。

## 3. Rekey journal 身份约束

- [x] 3.1 **[高优先级][实现]** 为 manifest entry 引入受限 kind/版本校验，并在预检、加载和恢复前按规范 env/text/config 布局及 config index 重新验证 identity，拒绝控制文件、sidecar 和未知路径。验证：运行 `go test ./internal/storage -run '^$'`。
- [x] 3.2 **[高优先级][测试]** 覆盖旧/未知 manifest version、sync-state/lock/journal/sidecar identity、未索引 config 和合法各 kind 的 rollback/roll-forward，断言非法 journal 零文件变更且保留材料。验证：运行 `go test ./internal/storage -run 'TestRekey(Manifest|Recovery).*Identity' -count=1`。

## 4. 递归删除准备边界

- [x] 4.1 **[高优先级][实现]** 将 `securefs.RemoveTree` 改为完整预检、同 parent 私有 quarantine rename、inode 复核和 descriptor-relative 清理；准备失败时不删除原目录对象。验证：运行 `go test ./internal/securefs -run '^$'`。
- [x] 4.2 **[高优先级][测试]** 用预检后替换文件/目录为 symlink 或不同 inode 的同步屏障测试，断言原树零删除、根外哨兵不变，并覆盖成功 quarantine cleanup。验证：运行 `go test ./internal/securefs -run 'TestRemoveTree.*(Concurrent|Quarantine|Zero)' -count=1`。

## 5. 回归与发布门禁

- [x] 5.1 **[高优先级][测试]** 运行跨模块回归，覆盖 provider rollback、session/rekey、fallback、manifest recovery 和 text group 删除，并确认无测试 sidecar/quarantine 残留。验证：运行 `go test -count=1 ./internal/provider ./internal/session ./internal/storage ./internal/securefs ./internal/text ./internal/config`。
- [x] 5.2 **[高优先级][验证]** 执行格式、静态检查、构建与 race 测试，并复核工作区仅包含预期实现/测试/规划文件。验证：`gofmt -l .` 无输出，且 `go vet ./...`、`go build ./...`、`go test -race -count=1 ./...` 全部通过。
