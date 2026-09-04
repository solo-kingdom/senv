# senv 多视角代码 Review

- 日期：2026-09-03
- 范围：CLI、TUI、MCP、加密与 session、env/text/config 存储、server sync、CI/发布
- 目标：提升产品体验、性能、安全性与工程质量
- 规模：约 27K LOC；Go 1.24.9

## 1. 结论摘要

项目已有较好的安全基础：AES-256-GCM 使用随机 nonce，PBKDF2 参数已版本化；敏感持久化文件通常使用 `0600/0700`；session 放入运行时临时目录；server 有鉴权、限流、请求大小限制和乐观锁；TUI 默认遮蔽 env 值且搜索不匹配值。fresh race 测试、vet、构建和格式检查也都通过。

但当前仍有 8 项应优先处理的 P1 问题，其中最紧急的是 `passwd`/rekey 故障可能破坏整个 vault、同步条目路径穿越、敏感写入跟随符号链接、session 撤销/缓存介质不符合安全语义，以及明文 text 导出权限过宽。另有一组 CLI 正确性问题会静默给用户错误结果；性能瓶颈主要来自重复 PBKDF2、全量解密/渲染和同步重复扫描，目前有明确静态证据，但应先补 benchmark 再设性能目标。

本次未发现满足 P0（无额外前提即可导致全库明文泄露或任意代码执行）的缺陷。

### 优先级定义

| 优先级 | 定义 | 建议时限 |
|---|---|---|
| P0 | 可直接造成严重泄露/代码执行，且利用前提很低 | 立即停发并修复 |
| P1 | 明确的安全边界、撤销语义、数据一致性或秘密暴露问题 | 下一补丁版本 |
| P2 | 重要正确性、条件性安全、可扩展性或工程质量问题 | 1–2 个迭代 |
| P3 | 深度加固、文档与长期维护改进 | 排入后续计划 |

## 2. P1：下一补丁版本处理

### P1-1 Server sync 条目标识可路径穿越

**证据**

- 服务端 `validateEntry` 只限制字段长度，不校验 kind 白名单或 `grp/key` 路径语义：`internal/server/store/store.go:247-265`。
- 客户端把远端 `grp/key` 直接交给 `filepath.Join`：`internal/provider/server_state.go:62-75`。
- `apply` 随后直接删除或写入该路径：`internal/provider/server_state.go:171-186`。
- 一次性探针使用 `Entry{Kind: "config", Key: "../escaped"}`，实际在 `dataPath` 外创建了 `escaped.enc`；探针通过后已删除，工作区保持干净。

**影响与前提**

持有同一 vault token 的恶意客户端、被攻陷的同步服务，或被污染的服务端数据，可让其他客户端在 vault 外写入/删除可达的 `.enc` 文件。由于 config 文件名会追加 `.enc`，不应夸大为无条件覆盖任意文件；但它可以与符号链接问题组合，且已经越过同步缓存边界。

**建议**

1. 服务端和客户端共用严格的 entry identity 校验：kind 白名单；按 kind 约束 `grp/key` 必填/必须为空；拒绝空值、`.`、`..`、绝对路径、`/`、`\\`、NUL。
2. `entryPath` 改为 `(string, error)`，完成 `filepath.Clean` 后用 `filepath.Rel(base, candidate)` 做 containment 检查。
3. 客户端必须防御服务端：不能只依赖服务端输入校验。
4. 删除与写入都拒绝目标或父路径中的符号链接。

**验收测试**

覆盖所有 kind，以及 `../x`、`a/../../x`、绝对路径、Windows 分隔符、空 grp/key、未知 kind；断言目标始终位于预期根目录内。

### P1-2 config 名称可越出 data 目录

**证据**

- `config create` 未校验名称：`cmd/config.go:45-69`。
- `config.Manager.Create/Delete` 直接使用名称：`internal/config/manager.go:56-90,259-273`。
- storage 保存/读取使用 `name + ".enc"`：`internal/storage/manager.go:565-595`。
- 同类缺口还存在于 env/text group：`internal/env/manager.go:243-258` 的 `AddGroup` 未调用校验；`internal/text/manager.go:323-358` 直接用字符串拼 group 路径，并在删除时 `RemoveAll`。
- 项目已有 `storage.ValidateName`，但上述路径没有统一调用；且当前校验仍允许空串和 `.`：`internal/storage/validate.go:15-27`。
- 一次性探针调用 `Create("../escaped", ...)`，实际在 data 目录外生成 `escaped.enc`。

**影响与前提**

本地 CLI 使用者本就拥有较高本机权限，因此单独看不是提权；P1 的依据是 storage manager 的路径边界和数据一致性被破坏，而且 group 写入口同时暴露给 CLI/TUI/MCP。远端条目直接越界由 P1-1 覆盖；恶意索引或名称还可能使删除操作移除 data 目录外的 `.enc` 文件或目录。

**建议**

在 manager 与 storage 两层校验，不要只在 Cobra 层校验。config/group/text key 使用统一的“非空、不是 `.`/`..`、不含 NUL、单路径段”规则。索引加载时分别校验 map key、`ConfigFile.Name` 与 `EncryptedFile`，并验证三者映射一致。所有会构造路径的 create/read/load/list/edit/delete、AddGroup/DeleteGroup 与 storage 公开方法必须使用同一 helper；任何 `RemoveAll` 前还需做 containment 与 no-symlink 检查。

### P1-3 敏感写入会跟随符号链接

**证据**

- `tightenFile` 遇到非普通文件直接返回 nil：`internal/storage/secure_write.go:45-57`。
- 随后的 `os.WriteFile` 会跟随符号链接：`internal/storage/secure_write.go:16-23`。
- `EnsurePrivateDir` 使用 `os.Stat`，父目录符号链接也会被视作普通目录：`internal/storage/secure_write.go:29-42`。
- sync 的 apply/metadata 写入还绕过 `WriteSensitiveFile`：`internal/provider/server_state.go:171-206`。
- 一次性探针实际证明 `WriteSensitiveFile` 可经 `settings.json` symlink 覆盖链接目标。

**建议**

实现 no-follow 原子写：以可信根目录的 directory fd 为锚，使用 `openat/openat2`、`O_NOFOLLOW`、`RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS` 等平台能力创建同目录 `0600` 临时文件，写入并 `fsync` 后安全 rename；其他平台应提供保守等价实现或 fail closed。不要采用“先 Lstat 全链、再普通写/rename”的 check-then-use 方案，否则仍有 TOCTOU。sync、metadata、state 和普通 storage 必须复用同一安全写原语。

### P1-4 MCP 只在启动时鉴权，session 到期/clear 无法撤销已启动进程

**证据**

- `mcp serve` 启动时只调用一次 `getManagersForMCP`：`cmd/mcp.go:31-43`。
- managers 长期持有派生 key，handler 不再检查 session：`cmd/mcp.go:62-100`。
- `session clear` 只删除 cache 文件：`internal/session/manager.go:228-234`。

**影响**

如果 agent/MCP 子进程比 session 活得更久，用户设置的超时或主动 clear 不会撤销其读取、导出和修改秘密的能力。这与 session 的产品语义不一致。

**建议**

每个 MCP 请求前验证 session ID、到期时间、boot ID、salt 和 key；失败后清空内存 key 并返回需要重新启动 session 的错误。可缓存最近一次校验数百毫秒以降低 I/O，但不能跨越 expiry。`never` session 保持现有语义。

### P1-5 session cache 未验证 runtime dir 是否为 memory-backed，且无 XDG 时回退 `/tmp`

**证据**

- 代码注释和安全文档承诺 session cache 对所有 timeout 都必须只位于 tmpfs：`internal/session/cache.go:39-42`、`docs/SECURITY.md:88`。
- `XDG_RUNTIME_DIR` 非空时，代码只拼接并创建该路径，不验证 backing filesystem：`internal/session/cache.go:43-49`；该环境变量也可能被配置到磁盘目录。
- `XDG_RUNTIME_DIR` 为空时回退到 `os.TempDir()/senv-<uid>`：`internal/session/cache.go:51-66`；测试还明确覆盖 `never` session 的该 fallback：`internal/session/cache_test.go:67-96`。
- cache 保存的是可直接解密 vault 的 base64 派生 key：`internal/session/types.go:19-28`。

**影响与前提**

XDG 路径和 `/tmp` 在部分系统、容器或自定义挂载中都可能是磁盘文件系统，不保证重启清除，也可能留下可恢复的数据块。文件/目录的 `0600/0700` 可阻止其他普通用户直接读取，但不能兑现“不落持久盘”的安全承诺，尤其影响 `never` 模式和磁盘取证/备份场景。

**建议**

对 XDG 路径与 fallback 路径都验证介质；无法确认 memory-backed 时优先 fail closed，至少 `never` 必须如此。也可提供明确的 opt-in disk fallback，并打印醒目警告。不能把路径名称或位置等同于 tmpfs。测试分别模拟 disk-backed `XDG_RUNTIME_DIR` 和 disk-backed `/tmp`，验证默认拒绝、显式 opt-in、警告和重启清理语义。

### P1-6 metadata 中 KDF 迭代次数无上限，可造成持久 CPU DoS

**证据**

- 任意正数都会由 `EffectiveIterations` 原样返回：`internal/storage/types.go:23-28`。
- 该值直接进入 PBKDF2：`internal/storage/manager.go:201-218,516-525`、`internal/crypto/keyderive.go:30-32`。
- metadata 可来自同步服务或仓库。攻击者可写入极大整数，让每次解锁/验证长期占用 CPU。

**影响与评级依据**

前提是攻击者已能写 metadata（同 vault token、被攻陷的服务或仓库写权限），因此它不是低前提的 P0。这里保留 P1，是因为项目把同步服务/仓库内容当作需防御的输入，且客户端在读取公开成本参数后会主动执行无上限计算；若产品明确把这些来源定义为完全可信，可降为 P2，但仍必须 fail-fast。

**建议**

按 metadata version 定义允许范围并在任何派生前 fail-fast。保留 `0 => 100000` 的 legacy 兼容；当前版本只接受明确的安全范围或已知离散值。测试 `0`、负数、过低值、当前默认值和 `MaxInt`，测试不得真的执行超大 PBKDF2。

### P1-7 text 明文导出默认创建 0644 文件

**证据**

- `GetToFile` 使用 `os.WriteFile(..., 0644)`：`internal/text/manager.go:256-275`。
- 同一函数用 `outputPath[:strings.LastIndex(outputPath, "/")]` 取父目录：`internal/text/manager.go:265-266`；basename（如 `-o key.pem`）的 LastIndex 为 -1，会触发 slice-bounds panic。
- 一次性探针导出 `PRIVATE_KEY` 到绝对路径后实际 mode 为 `0644`。
- config export/install 在新建目标时请求 `0644`（实际 mode 为 `0644 &^ umask`）；覆盖现有文件时 `os.WriteFile` 保留原 mode：`internal/config/install.go:176-185`。config 新建目标是否默认私有需要单独产品决策。

**影响**

在默认 umask 允许时，同机其他用户可读取导出的私钥、token 或证书私钥。README 明确把私钥列为 text 典型场景，因此 text 应默认按秘密处理；常见的 basename 输出还会直接使 CLI/TUI 崩溃。

**建议**

先通过 `expandHome(outputPath)`（或统一路径解析 helper）展开 `~/...`，再用 `filepath.Dir` 处理 basename、相对子目录和绝对路径；结果为 `.` 时不创建目录。text 导出默认 `0600`，使用安全原子写并收紧已存在文件权限；增加 `--mode` 作为显式放宽方式。config 继续保留已有目标 mode，新建目标默认 `0600`，由用户显式选择 `0644`，并在变更默认值时提供迁移说明。

### P1-8 `passwd`/Rekey 不是可恢复事务，失败可破坏整个 vault

**证据**

- `Rekey` 先把所有 `.rekey-tmp` 逐个 rename 覆盖原密文，最后才更新 metadata：`internal/storage/rekey.go:31-58`。metadata 写失败时，密文已经使用新 key，而 metadata 仍记录旧 salt/KDF；新 salt 只在进程内，失败返回后可能不可恢复。
- `rekeyCommit` 可在多文件 rename 中途失败：`internal/storage/rekey.go:163-171`；此时所谓 rollback 只删除尚存的临时文件，并不恢复已覆盖的旧密文，`oldKey` 参数没有被使用：`internal/storage/rekey.go:173-180`。vault 会混合新旧 key。
- 预检的 `WalkDir` callback 在收到文件系统错误时返回 nil：`internal/storage/rekey.go:84-87`；`LoadConfigIndex` 失败也被忽略：`internal/storage/rekey.go:121-138`。未枚举文件可能保留旧 key，但 metadata 仍更新到新 key。
- 命令帮助明确承诺失败时保留原加密：`cmd/passwd.go:15-20`；现有测试只覆盖成功和错误旧 key，没有注入 commit/metadata/crash 故障：`internal/storage/rekey_test.go`。

**影响**

磁盘满、权限变化、I/O 错误、进程崩溃或单次 rename/metadata 写失败，都可能让 vault 进入无法由单一密码解锁的状态。这是明确的数据完整性与可恢复性缺陷，优先级高于一般性能优化。

**建议**

1. 所有遍历与 index 错误必须 fail closed。
2. 设计 durable journal/manifest 的两阶段事务：保留旧密文和旧 metadata；完整写入并 fsync 全部新文件；记录阶段；切换 metadata；最后清理旧版本。
3. 启动时检测并恢复未完成事务，保证任意故障点后“旧状态完整可解锁”或“新状态完整可解锁”至少成立一个。
4. 对每个 write/rename/fsync/metadata stage 注入失败，并增加子进程 crash-recovery 测试。仅使用临时文件后逐个 rename 不能提供多文件 + metadata 的整体原子性。

## 3. P2：重要正确性、安全设计与性能

### P2-1 CLI 会静默产生错误结果

以下问题建议作为一组修复并增加命令级测试：

1. `env get --refresh` 和 `text get --refresh` 注册了 flag，却没有调用 `autoPull`：`cmd/env.go:52-83,416`、`cmd/text.go:115-145,364`。用户会读取旧数据。
2. `text get -d --copy/-o` 先算出 decoded `value`，随后调用 manager 再读 raw 值：`cmd/text.go:124-140`。终端输出和文件/剪贴板结果不一致。
3. `senv env list` 文档称不指定 group 时列全部，但持久 flag 默认 `default`，实现用 `listGroup := envGroup`：`cmd/env.go:29,139-160`。实际只列 default。
4. `text set` 只要求最少 1 个参数并忽略第 3 个及以后参数；root/env/text shorthand 也只取 `valueArgs[0]`：`cmd/text.go:70,88-90`、`cmd/shorthand.go:48-77`。应拒绝多余参数，避免秘密被截断。
5. 未知根/env/text 命令返回 help 的 nil error：`cmd/root.go:23-34`、`cmd/env.go:16-24`、`cmd/text.go:20-28`。实测两个场景均 exit 0，破坏脚本可靠性。

建议为 CLI 建立 table-driven 黑盒测试，断言 stdout、stderr、exit code、是否触发 sync，以及 raw/decoded 数据一致性。

### P2-2 列表与 TUI 搜索吞掉损坏条目

`env.Manager.List("")` 跳过无法读取的 group：`internal/env/manager.go:167-190`；`env.Manager.Export` 同样跳过无法读取的 active group：`internal/env/manager.go:195-213`，可能让 shell 获得缺失变量；`text.Manager.List` 跳过无法解密的 entry：`internal/text/manager.go:110-126`；TUI 搜索也继续忽略读取错误。损坏/密钥脱节会被伪装为“没有数据”。

建议返回“结果 + per-item error”或聚合错误，CLI/TUI 显示 partial result 警告和损坏项名称，绝不能静默消失；错误消息仍须避免输出明文。

### P2-3 密文没有绑定 kind/group/key 上下文

AES-GCM 的 AAD 为 nil：`internal/crypto/crypto.go:48,81`。同一 vault key 下，攻击者若可修改后端/文件，可把一个合法密文调包到兼容的另一路径。单个 ciphertext 的机密性与 GCM 完整性没有失效，风险是上下文调包与重放，不应描述为“破解 AES”。

建议设计版本化 envelope，将 vault ID、kind、group、key、schema version 作为 AAD；提供原子迁移和旧格式只读兼容测试。

### P2-4 同步的 config index 可改变本机默认导出目标

`config_index.json` 参与同步：`internal/provider/server_state.go:19,72-73,160-164`；`Export` 在未指定路径时信任其中的 `TargetPath`：`internal/config/manager.go:212-247`。远端因此可静默改变下一次本机导出位置。任意目标路径本身是产品功能，真正问题是“机器本地信任决策被同步覆盖”。

建议把 target 映射拆为 machine-local 配置，或在远端 target 变化后要求确认；对系统目录、shell 启动文件等高风险目标给出明显提示。

### P2-5 MCP 暴露批量明文能力，缺少最小权限模式

`envList` 返回值、`envExport` 批量返回明文：`cmd/mcp.go:221-253`。README 已披露 agent 获得 senv 读写能力，因此这不是未声明漏洞，但默认能力面过大。

建议提供 read-only、metadata-only list、group/key allowlist、禁止 bulk export、敏感操作确认等 profile；tool description 明确标注是否返回 secret value；审计记录调用工具、key 名和结果，不记录明文。

### P2-6 性能：先建立基准，再处理三个热点簇

#### A. 重复 PBKDF2

无 session 的一次命令可能先 `VerifyPassword`，随后 manager 每次操作再次按 password 派生；env manager 与 storage 都存在派生路径：`internal/storage/manager.go:201-218,516-525`、`internal/env/manager.go:39-66`。`session start` 也会验证后再次派生。600k 次迭代使重复成本可见。

建议 auth 成功后在命令生命周期只保留 `SecureKey`，所有 manager 接收 key；显式清零临时 password/key。先增加 `BenchmarkPasswordPath` 和 KDF 调用计数测试。

#### B. 全量解密与渲染

- `text.List` 为得到 size/time 解密每个正文：`internal/text/manager.go:110-126`。
- TUI 注释称 lazy，实际 `Init` 遍历初始化全部三个 tab：`internal/tui/model.go:48-57`。
- env tab 同时调用 `ListGroups` 与 `List("")`，并读取所有值：`internal/tui/env_tab.go:103-137`。
- search view 渲染全部结果而非 viewport：`internal/tui/search.go:195-213`。

建议把 text metadata 放入受认证的小型 index（或独立加密 metadata），TUI 首屏只加载当前 tab/current group，列表使用 viewport/虚拟化；以 1k/10k entries 的启动时间、按键延迟和内存为验收指标。

#### C. 同步重复扫描且锁覆盖网络等待

`SyncWithReport` 先 collect 统计 dirty，随后 pull/push 各自再次 collect：`internal/provider/server.go:355-378`；`AutoPush` dirty scan 后，`push` 再 collect：`internal/provider/server_auto.go:76-108`。外层 sync lock 在 API 网络调用期间一直持有，因此其他 sync/auto-sync 尝试只能等待或跳过；普通 env/text/config 本地操作不使用该锁，不应描述为被阻塞。

先安全地消除同一次调用中的重复 collect，并用 benchmark/调用计数验证。进一步缩小网络阶段锁范围会允许跨进程同步事务并发，必须先设计同步事务序列化或租约、state CAS、普通 mutation 与 snapshot 的协调，以及提交失败恢复；不能只靠末尾 hash 复核，否则可能破坏现有乐观锁和 state 语义。优化前保留冲突与并发写测试。

### P2-7 长驻 MCP/TUI 的成功写入在进程退出前不会自动 push

自动 push 只挂在 root 的 `PersistentPostRun`：`cmd/root.go:24-26`、`cmd/autosync.go:79-94`；`mcp serve` 和 TUI 返回前不会触发，各 mutation 也没有 per-write push。由于 AutoPush 在代码中明确是 best-effort，本地写入已经持久化且乐观锁避免静默覆盖，因此这是远端可见性延迟、冲突概率和误导性 UX，不是本地数据丢失，定为 P2。

建议 MCP 响应明确区分 `local_saved / sync_pending / sync_error`，并按产品契约决定是否在响应前做有预算的 push。TUI 可显示 `synced / pending / conflict / offline`，但防抖后台 push 必须等 P1-3 的原子写和 mutation/snapshot 协调完成后再引入，否则 collect 可能与普通写入竞态。退出时可做有时限 flush，但不能只依赖退出。

### P2-8 CI 没有执行仓库已经支持的质量门禁

PR CI 和 release test 都只运行 `go test ./...`：`.github/workflows/ci.yml:1-15`、`.github/workflows/release.yml:8-16`。本地 Makefile 已支持 race、vet、coverage 和 security，但 CI 未执行。当前总覆盖率 54.7%，`cmd` 27.1%、`internal/env` 31.1%，正好对应本次多个未覆盖的 CLI/边界缺陷。

建议 CI 至少增加：`go test -race -count=1 ./...`、`go vet ./...`、`gofmt -l` 必须为空、`go build ./...`、`govulncheck ./...`、关键包覆盖率下限；server migration 和 main package 当前为 0% 应补 smoke test。race 当前全包约需 2–3 分钟，可拆 job 并并行。

## 4. P3：深度加固与维护

1. **文档不一致**：README 仍称 PBKDF2 100,000 次：`README.md:305`；实现与 `docs/SECURITY.md:12,103-105` 表明新 vault 为 600,000，旧 vault 才是 100,000。应以单一来源生成文档，避免用户误判安全参数。
2. **编辑器异常退出残留明文**：正常路径会清理 `0600` 临时文件，但 kill -9/断电可能残留。建议放入用户私有 runtime dir并在启动时清理过期 `senv-*.tmp`。若要为 vim 禁用 swap/backup，应设计结构化 editor 参数配置；当前 `exec.Command(editor, path)` 不支持带参数的 `$EDITOR`，也不能为此改成不安全的 shell 字符串拼接。
3. **GitHub Actions 只按 tag 引用第三方 action**：release job 具有 `contents: write`。建议把 `actions/*` 和 `softprops/action-gh-release` 固定到 commit SHA，并由 Dependabot/Renovate 更新。
4. **安装校验只保证同源完整性**：archive 和 checksum 来自同一 GitHub release；可进一步对 checksums 签名或使用 GitHub artifact attestation/SLSA provenance。

## 5. 推荐实施顺序

### 阶段 0：先保证改口令可恢复

1. 让 rekey 的遍历/index 错误全部 fail closed。
2. 引入 durable journal/备份和启动恢复。
3. 用故障注入证明任意失败点后旧/新状态至少一个完整可解锁。

在该阶段完成前，应在 `passwd` 前强提示用户备份；不能继续承诺“失败时原加密一定保留”。

### 阶段 A：文件系统边界（先做）

1. 引入统一 `SafePathSegment`、entry schema 校验与 containment helper。
2. 修复 server/client 双边 entry 校验和 config name 校验。
3. 引入 no-follow 原子安全写，替换 storage/provider 的直接写入。
4. 把 text export 默认权限改为 `0600`。

这四项应在同一安全补丁版本交付，因为路径穿越与 symlink 可组合。

### 阶段 B：鉴权与同步语义

1. MCP per-request session 校验与撤销。
2. 对 XDG runtime 与 `/tmp` fallback 的介质验证采用 fail-closed 或明确 opt-in 策略。
3. KDF 参数范围校验。
4. MCP 最小权限 profile。
5. 完成原子写和 mutation/snapshot 协调后，再实现 MCP/TUI push 状态与后台同步。

### 阶段 C：正确性与可观测性

修复 refresh、decode output、list 默认组、参数截断和未知命令退出码；把 partial read error 显示到 CLI/TUI；新增黑盒命令测试。

### 阶段 D：有指标的性能优化

先增加 1k/10k fixture benchmark，再优化 key 生命周期、metadata list、TUI lazy load/viewport 和 sync snapshot。建议目标：TUI 1k 条首屏 <200ms、按键 p95 <50ms；无变更 auto-push 不做网络请求且只扫描一次；同一命令最多一次 PBKDF2。

## 6. 验证记录

本次执行并通过：

- `go test ./...`
- `go vet ./...`
- `go test -race -count=1 ./...`
- `go build ./...`
- `gofmt -l .`（无输出）
- `go test -count=1 -coverprofile=... ./...`：总覆盖率 **54.7%**；`cmd` **27.1%**；`internal/env` **31.1%**
- 未知根命令与未知 env 命令复现：均 `exit=0`，help 写入 stdout
- 4 个自动清理的一次性测试探针：config traversal、remote entry traversal、symlink overwrite、text export `0644` 均被实际复现；探针后 `git status --short` 为空

未能执行：`govulncheck ./...`，因为当前环境未安装该工具。这不是“未发现依赖漏洞”；应由 CI 安装固定版本后执行。

## 7. 建议新增的最小回归测试集

- `internal/provider`：所有远端 entry identity 非法矩阵、containment、父/目标 symlink、删除路径。
- `internal/config`/`internal/env`/`internal/text`/`internal/storage`：config/group/text key 的 create/read/load/list/edit/delete/AddGroup/DeleteGroup/Save/Load 全链路非法名称，尤其覆盖空串、`.`、`..`、NUL 和 `RemoveAll` containment；config index 分别覆盖 map key、Name、EncryptedFile。
- `internal/session`/`cmd`：短 session 到期与 clear 后，已启动 MCP 请求必须失败；`never` 不误撤销；disk-backed `XDG_RUNTIME_DIR` 与 disk-backed `/tmp` 默认均不落 key，显式 opt-in 时有警告和清理。
- `cmd`：get refresh、decode-to-file/copy、无 group list all、extra args、unknown command non-zero。
- `internal/text`：basename/相对/绝对/`~/` 导出不 panic，新建和覆盖文件均为 `0600`。
- `internal/storage`：rekey 每个 write/rename/fsync/metadata 阶段的故障注入和子进程 crash recovery；KDF 非法范围 fail-fast，legacy `0` 继续兼容。
- `internal/tui`：长驻 mutation 进入 pending 并完成 push；1k/10k view benchmark。
- `internal/provider`：一次 sync/auto-push 的 collect 次数与网络调用次数断言。
