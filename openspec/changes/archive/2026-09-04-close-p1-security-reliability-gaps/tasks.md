## 1. 安全文件系统基础

- [x] 1.1 **[高优先级][实现]** 建立 `internal/securefs` 的错误类型、可信根接口与 `ValidateSegment` 跨平台规则；验证：运行 `go test ./internal/securefs -run '^$'` 确认包可编译。
- [x] 1.2 **[高优先级][测试]** 为安全 segment 增加空值、`.`、`..`、NUL、冒号、双平台分隔符、绝对路径、卷标与合法名称矩阵；验证：运行 `go test ./internal/securefs -run TestValidateSegment -count=1`。
- [x] 1.3 **[高优先级][实现]** 实现 Linux `openat2` 优先及 Unix/macOS 逐段 `openat(O_NOFOLLOW)` 的可信根遍历、读取和 containment 后端；验证：运行 Linux 包测试编译，并用 `GOOS=darwin go test -c ./internal/securefs` 完成跨平台编译后清理产物。
- [x] 1.4 **[高优先级][测试]** 增加目标/中间父目录 symlink、根外逃逸、根替换与 unsupported backend 契约测试；验证：运行 `go test ./internal/securefs -run 'TestRoot|TestNoFollow|TestContainment' -count=1`。
- [x] 1.5 **[高优先级][实现]** 实现同目录独占临时文件、完整写入、mode 收紧、file/dir fsync 与 `renameat` 的 `AtomicWrite`；验证：运行 `go test ./internal/securefs -run '^$'` 并用 `go vet ./internal/securefs`。
- [x] 1.6 **[高优先级][测试]** 为 `AtomicWrite` 增加 write/fsync/rename 故障注入、完整旧/新值、0600、保留更严格既有 mode 和临时文件清理测试；验证：运行 `go test ./internal/securefs -run TestAtomicWrite -count=1`。
- [x] 1.7 **[高优先级][实现]** 实现受根约束的 `Remove`、`Rename` 与 handle-relative 递归删除，不调用裸路径 `RemoveAll`；验证：运行 `go test ./internal/securefs -run '^$'`。
- [x] 1.8 **[高优先级][测试]** 增加普通删除、非法 segment、父/目标 symlink、递归删除零副作用与根外哨兵测试；验证：运行 `go test ./internal/securefs -run 'TestRemove|TestRemoveTree|TestRename' -count=1`。

## 2. Stage 0：可恢复 rekey

- [x] 2.1 **[高优先级][实现]** 引入 vault 级跨进程 mutation lock，并提供 storage mutation/rekey 共用的加锁边界；验证：运行 `go test ./internal/storage -run '^$'`。
- [x] 2.2 **[高优先级][测试]** 增加同 vault 串行、不同 vault 独立、锁持有进程退出释放及超时错误测试；验证：运行 `go test ./internal/storage -run TestVaultMutationLock -count=1`。
- [x] 2.3 **[高优先级][实现]** 定义版本化 rekey manifest、事务阶段、规范 entry identity/hash 与 securefs 原子读写；验证：运行 `go test ./internal/storage -run '^$'` 并检查 manifest 不含明文/key 字段。
- [x] 2.4 **[高优先级][测试]** 增加 manifest round-trip、版本拒绝、损坏 JSON、hash 不匹配、0600 与 symlink 目标测试；验证：运行 `go test ./internal/storage -run TestRekeyManifest -count=1`。
- [x] 2.5 **[高优先级][实现]** 重写 rekey 预检，使 WalkDir、env/text 枚举、config index 解析/身份校验与任一解密错误全部 fail closed；验证：运行 `go test ./internal/storage -run TestRekeyPreflight -count=1`。
- [x] 2.6 **[高优先级][测试]** 注入目录权限/I/O、损坏密文、坏 config index、遗漏文件与非法身份，断言零写入；验证：运行 `go test ./internal/storage -run 'TestRekeyPreflight|TestRekeyNoMutationOnPreflightError' -count=1`。
- [x] 2.7 **[高优先级][实现]** 实现 PREPARE：生成 transaction ID、同目录 `.new` 密文、逐文件/目录 fsync，并在全部准备完成后持久化 PREPARED；验证：运行 `go test ./internal/storage -run '^$'`。
- [x] 2.8 **[高优先级][测试]** 对 encrypt/write/file-fsync/dir-fsync/manifest 各故障点注入失败，断言旧 metadata/密文完整可解；验证：运行 `go test ./internal/storage -run TestRekeyPrepareFailures -count=1`。
- [x] 2.9 **[高优先级][实现]** 实现 SWITCH_DATA、SWITCH_METADATA、COMMITTED 与 CLEANUP 状态转换，保留 `.old` 直到新 metadata durable；验证：运行 `go test ./internal/storage -run '^$'`。
- [x] 2.10 **[高优先级][测试]** 注入每次 rename、阶段 manifest、metadata write/fsync 和 cleanup 失败，验证旧或新状态至少一套完整可解；验证：运行 `go test ./internal/storage -run TestRekeyCommitFailures -count=1`。
- [x] 2.11 **[高优先级][实现]** 实现 recovery gate：按 metadata/entry hash 自动 rollback 或 roll-forward，无法判定时保留材料并阻断普通访问；验证：运行 `go test ./internal/storage -run '^$'`。
- [x] 2.12 **[高优先级][测试]** 覆盖 PREPARED、数据切换一半、DATA_SWITCHED、metadata 已切换未标记、COMMITTED 清理中和 manifest 损坏恢复；验证：运行 `go test ./internal/storage -run TestRekeyRecovery -count=1`。
- [x] 2.13 **[高优先级][实现]** 把 recovery gate 与 mutation lock 接入 env/text/config 写入、sync apply 和 `passwd`，并在成功后清理事务材料；验证：运行 `go test ./internal/{storage,env,text,config,provider} -run '^$'`。
- [x] 2.14 **[高优先级][测试]** 增加 rekey 与 env/text/config/sync 并发 mutation 测试，断言不出现未纳入 manifest 的条目；验证：运行 `go test -race ./internal/storage ./internal/env ./internal/text ./internal/config ./internal/provider -run 'Test.*Rekey.*Concurrent' -count=1`。
- [x] 2.15 **[高优先级][实现]** 增加仅测试构建可用的 rekey crash failpoint/子进程夹具，不在生产路径暴露控制入口；验证：运行 `go test ./internal/storage -run TestRekeyCrashHarness -count=1`。
- [x] 2.16 **[高优先级][测试]** 在每个 durable 阶段强杀子进程并由新进程恢复，验证单一密码可解全部条目且无混合状态；验证：运行 `go test ./internal/storage -run TestRekeyCrashRecovery -count=1`。

## 3. Stage A：本地身份与持久化边界

- [x] 3.1 **[高优先级][实现]** 将 metadata、settings、config index 和通用敏感写入迁移到 securefs，并移除会跟随链接的普通写路径；验证：运行 `go test ./internal/storage -run '^$'` 并搜索受影响路径无新增 `os.WriteFile`。
- [x] 3.2 **[高优先级][测试]** 为 metadata/settings/index 增加目标和父 symlink、0644 收紧、原子中断与目录权限测试；验证：运行 `go test ./internal/storage -run 'Test.*(Metadata|Settings|ConfigIndex).*(Symlink|Permission|Atomic)' -count=1`。
- [x] 3.3 **[高优先级][实现]** 在 env manager/storage 全入口接入安全 group 与 POSIX key 双重校验和受根路径操作；验证：运行 `go test ./internal/env ./internal/storage -run '^$'`。
- [x] 3.4 **[高优先级][测试]** 覆盖 env create/read/list/export/AddGroup/activate/deactivate/delete 的非法 identity 矩阵与历史坏 group 诊断；验证：运行 `go test ./internal/env ./internal/storage -run 'Test.*Env.*(Validation|Traversal|Identity)' -count=1`。
- [x] 3.5 **[高优先级][实现]** 在 text manager/storage 全入口接入 group/key 校验，并用 handle-relative 删除替代 group `RemoveAll`；验证：运行 `go test ./internal/text ./internal/storage -run '^$'`。
- [x] 3.6 **[高优先级][测试]** 覆盖 text set/get/list/AddGroup/DeleteGroup 的空值、点目录、双平台分隔符、NUL、symlink 与根外哨兵；验证：运行 `go test ./internal/text ./internal/storage -run 'Test.*Text.*(Validation|Traversal|DeleteGroup)' -count=1`。
- [x] 3.7 **[高优先级][实现]** 在 config manager/storage 全入口验证 name/group，并在 index loader 校验 map key、Name、Group、EncryptedFile 一致性与 legacy 空值归一化；验证：运行 `go test ./internal/config ./internal/storage -run '^$'`。
- [x] 3.8 **[高优先级][测试]** 覆盖 config create/read/list/edit/export/install/delete、坏 index 三字段及 legacy 空 EncryptedFile；验证：运行 `go test ./internal/config ./internal/storage -run 'Test.*Config.*(Validation|Index|Traversal|Legacy)' -count=1`。

## 4. Stage A：同步条目与客户端防御

- [x] 4.1 **[高优先级][实现]** 建立共享 `internal/syncschema` kind 常量与 grp/key 组合验证，复用 secure segment 规则；验证：运行 `go test ./internal/syncschema -run '^$'`。
- [x] 4.2 **[高优先级][测试]** 增加五种合法 kind、未知 kind、缺失/额外字段和路径攻击矩阵；验证：运行 `go test ./internal/syncschema -count=1`。
- [x] 4.3 **[高优先级][实现]** 在 server 开启事务/创建 vault 前验证完整 push 批次并返回稳定 validation error；验证：运行 `go test ./internal/server/store -run '^$'`。
- [x] 4.4 **[高优先级][测试]** 增加合法+非法混合批次，断言不创建 vault、不推进 revision、不落任何条目；验证：运行 `go test ./internal/server/store -run TestPushEntryIdentity -count=1`。
- [x] 4.5 **[高优先级][实现]** 将 client `entryPath` 改为返回错误，pull 先验证整批，再通过 securefs apply/delete，成功后才提交 revision/state；验证：运行 `go test ./internal/provider -run '^$'`。
- [x] 4.6 **[高优先级][测试]** 模拟被攻陷 server 返回 traversal、绝对路径、Windows 分隔符、未知 kind、目标/父 symlink，断言缓存和 state 零变化；验证：运行 `go test ./internal/provider -run TestRemoteEntryIdentity -count=1`。
- [x] 4.7 **[高优先级][实现]** 将 provider metadata、sync state 和 config index apply 全部迁移到 securefs 原子写/删除；验证：运行 `go test ./internal/provider -run '^$'` 并检查相关文件无直接 `os.WriteFile`。
- [x] 4.8 **[高优先级][测试]** 注入 apply/state/metadata 的 write/fsync/rename 失败，断言 revision 不前进且旧缓存完整；验证：运行 `go test ./internal/provider -run 'Test.*Apply.*Failure|Test.*State.*Atomic' -count=1`。

## 5. Stage A：明文导出权限与路径

- [x] 5.1 **[高优先级][实现]** 实现共享导出路径解析与八进制 mode 校验，支持 basename、相对、绝对和 `~/`，拒绝特殊位；验证：运行相关包 `go test` 编译并用 `go vet ./cmd ./internal/text ./internal/config`。
- [x] 5.2 **[高优先级][测试]** 增加路径表和 mode `0600/0644/非法/特殊位` 单元测试，断言解析前无文件变化；验证：运行 `go test ./cmd ./internal/text ./internal/config -run 'Test.*(ExportPath|FileMode)' -count=1`。
- [x] 5.3 **[高优先级][实现]** 将 text CLI/TUI 导出接入 securefs，默认/覆盖收紧为 0600，并增加 CLI `--mode`；验证：运行 `go test ./cmd ./internal/text ./internal/tui -run '^$'`。
- [x] 5.4 **[高优先级][测试]** 覆盖 basename 不 panic、相对/绝对/home、0600、新旧 mode、目标/父 symlink 和 TUI 默认权限；验证：运行 `go test ./cmd ./internal/text ./internal/tui -run 'Test.*Text.*Export' -count=1`。
- [x] 5.5 **[高优先级][实现]** 将 config export/install/backup 接入安全原子写，新增 `--mode`，新文件默认 0600且不放宽更严格既有 mode；验证：运行 `go test ./cmd ./internal/config -run '^$'`。
- [x] 5.6 **[高优先级][测试]** 覆盖 config 新建/覆盖/backup 的默认 0600、显式 0644、既有 0400 保留、symlink 和原子故障；验证：运行 `go test ./cmd ./internal/config -run 'Test.*Config.*(Export|Install|Mode|Symlink)' -count=1`。

## 6. Stage B：KDF 输入边界

- [x] 6.1 **[高优先级][实现]** 用返回错误的版本化 KDF validator 替代无错误 `EffectiveIterations`，实现 legacy 0 与 `[100000,1000000]` 边界；验证：运行 `go test ./internal/storage ./internal/crypto -run '^$'`。
- [x] 6.2 **[高优先级][测试]** 覆盖缺失/0、负数、99999、100000、600000、1000000、1000001、JSON 溢出和 MaxInt；验证：运行 `go test ./internal/storage ./internal/crypto -run TestKDF.*Iterations -count=1`。
- [x] 6.3 **[高优先级][实现]** 将解锁、VerifyPassword、各 manager、session start、MCP 启动与 rekey 全部接入统一 validator；验证：运行 `go test ./cmd ./internal/storage ./internal/session -run '^$'`。
- [x] 6.4 **[高优先级][测试]** 增加 KDF 调用计数，证明非法 metadata 在 CLI/MCP/session/rekey 各入口均不调用 PBKDF2且不误报密码错；验证：运行 `go test ./cmd ./internal/storage ./internal/session -run TestInvalidKDFRejectedBeforeDerive -count=1`。

## 7. Stage B：session 介质与 MCP 撤销

- [x] 7.1 **[高优先级][实现]** 实现 build-tagged runtime filesystem probe，Linux 仅认可 tmpfs/ramfs，其他平台未知介质 fail closed；验证：运行本机包编译及 `GOOS=darwin go test -c ./internal/session` 后清理产物。
- [x] 7.2 **[高优先级][测试]** 通过 probe 注入覆盖 memory-backed、disk-backed、未知类型和查询错误；验证：运行 `go test ./internal/session -run TestRuntimeFilesystemProbe -count=1`。
- [x] 7.3 **[高优先级][实现]** 在 XDG 与 fallback cache 解析前强制介质验证，fallback 仅在安全介质创建随机 0700 目录，并用 securefs 写 cache；验证：运行 `go test ./internal/session -run '^$'`。
- [x] 7.4 **[高优先级][测试]** 覆盖 disk-backed XDG、disk-backed `/tmp`、安全 fallback、never/restart/duration、cache/父 symlink、随机数失败和零落盘；验证：运行 `go test ./internal/session -run 'Test.*Cache.*(Filesystem|Fallback|Symlink|Random)' -count=1`。
- [x] 7.5 **[高优先级][实现]** 将 MCP 启动授权改为非秘密 fingerprint，并提供统一 request wrapper 每次重载/校验 session、构造临时 managers、defer 清零 key；验证：运行 `go test ./cmd -run '^$'` 并确认 tool 注册均通过 wrapper。
- [x] 7.6 **[高优先级][测试]** 覆盖 duration expiry、clear、session ID 替换、boot ID 变化、salt/rekey 变化与有效 never 请求；验证：运行 `go test ./cmd -run TestMCPRequestSessionGuard -count=1`。
- [x] 7.7 **[高优先级][实现]** 统一 MCP 撤销错误与审计事件，确保失败发生在 auto-pull 和业务 manager 前且不记录 key/明文；验证：运行 `go test ./cmd ./internal/session -run '^$'`。
- [x] 7.8 **[高优先级][测试]** 对全部 MCP tools 做 guard 覆盖测试，断言撤销后无读值、无 mutation、无 sync，并检查错误/审计脱敏；验证：运行 `go test ./cmd -run 'TestAllMCPToolsGuarded|TestMCPRevocationNoSideEffects' -count=1`。

## 8. 文档、兼容与发布门禁

- [x] 8.1 **[高优先级][实现]** 更新 README、SECURITY、命令帮助和 release note：600k/legacy KDF、session memory-backed 限制、rekey 恢复、默认 0600及 `--mode` 迁移；验证：运行文档示例命令的 `--help` smoke test 并检查无 100k 新 vault 误述。
- [x] 8.2 **[高优先级][测试]** 增加 CLI 黑盒回归，覆盖 text/config `--mode`、unsafe runtime 错误、KDF 参数错误、rekey recovery 提示与非零退出码；验证：运行 `go test ./cmd -run TestSecurityBoundaryCLI -count=1`。
- [x] 8.3 **[高优先级][测试]** 运行完整非法 identity、symlink、故障注入和 crash-recovery 安全回归集，并确认临时探针/sidecar 均清理；验证：运行 `go test -count=1 ./internal/securefs ./internal/storage ./internal/env ./internal/text ./internal/config ./internal/provider ./internal/server/... ./internal/session ./cmd`。
- [x] 8.4 **[高优先级][验证]** 执行格式、静态检查、构建与全量测试；验证：`gofmt -l .` 无输出，且 `go vet ./...`、`go build ./...`、`go test ./...` 全部通过。
- [x] 8.5 **[高优先级][验证]** 执行全量 race 测试并复核工作区只含预期实现/测试/文档文件；验证：运行 `go test -race -count=1 ./...` 与 `git status --short`，不得遗留 crash fixture、临时二进制或 rekey sidecar。
