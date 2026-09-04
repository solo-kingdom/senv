## Context

见 [proposal.md](./proposal.md) 的动机与范围。当前实现有三个相互放大的边界缺口：文件路径由外部 identity 直接拼接且普通写会跟随符号链接；rekey 逐文件覆盖后才切 metadata，失败时没有真正回滚；MCP 在启动时把 derived key 固定在长驻 managers 中。与此同时，metadata/session/sync state 分布在 configPath 与可自定义 dataPath，二者可能位于不同文件系统，不能假设一次跨目录 rename 能提供 vault 级原子性。

设计受以下约束：继续支持 Linux/macOS 的本地 CLI；不改变 AES-GCM 密文、server API 或正常数据布局；旧 metadata 的 `kdf_iterations=0` 继续按 100,000 处理；无法证明路径、介质或事务状态安全时必须 fail closed。

## Goals / Non-Goals

**Goals:**
- 用一套可复用、无 TOCTOU 的受信根文件原语承载 storage、sync、metadata/state 与明文导出。
- 让 rekey 在任意注入错误或进程崩溃后可确定性恢复，普通入口永不观察混合 key 状态。
- 在 server/client 两端统一验证同步 entry schema，并在客户端保持独立防御。
- 让 session 的“仅 memory-backed”承诺可验证，让已启动 MCP 在 expiry/clear/rekey 后立即失权。
- 保持现有长期数据格式可读，并给出清晰的发布、恢复和回滚路径。

**Non-Goals:**
- 不引入新的加密 envelope、AAD 迁移、daemon、系统 keyring 或 MCP 权限 profile。
- 不优化 PBKDF2 次数、TUI 列表、sync 扫描或网络锁范围。
- 不允许默认或隐式的 disk-backed session fallback；需要该兼容策略时另立安全评审 change。
- 不把任意用户导出路径限制在 vault 根内；只保证目标解析和写入不被符号链接竞态劫持。

## Decisions

### 1. 建立 `internal/securefs` 受信根原语，而不是继续传递裸路径

引入内部包，核心抽象是已打开的可信目录 handle 与相对 segment 列表：

- `ValidateSegment`：统一拒绝空值、`.`、`..`、NUL、`:`, `/`, `\\`、绝对路径和卷标语义；env key 仍叠加 POSIX shell regex。
- `Root`：打开并持有 configPath、dataPath 或导出父目录的 directory handle；后续操作只接受相对 segment。
- `Read/AtomicWrite/Remove/Rename`：逐段以 no-follow 方式打开目录，最终操作相对于 directory handle 完成；所有候选路径另做 clean/relative containment 断言，作为防御性校验而非主要安全机制。
- Linux 优先使用 `openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS)`；不支持时及 macOS 使用逐段 `openat(O_DIRECTORY|O_NOFOLLOW)`、`fstatat(..., AT_SYMLINK_NOFOLLOW)`、`renameat/unlinkat` 的等价流程。无法建立等价保证的平台返回 unsupported/fail-closed 错误。
- 原子写在目标同目录以随机名、`O_CREAT|O_EXCL|O_NOFOLLOW` 创建临时文件，完整写入、设置最终 mode、`fsync(file)`、`renameat`、`fsync(parent)`。目标若是符号链接则拒绝；普通旧目标通过 rename 替换，不再“先 chmod 再普通 WriteFile”。
- 覆盖时最终 mode 取“请求 mode 与既有 mode 中不更宽者”；默认导出可把 0644 收紧为 0600，但不会把 0400 隐式放宽。显式 mode 只决定新文件或不比既有权限更宽的替换。

选择 directory-fd 锚定而不是 `Lstat → os.WriteFile`，因为后者在检查与使用间仍可被替换。也不只依赖字符串 `Rel`，因为 containment 不能阻止根内父目录变成符号链接。

### 2. identity 校验集中，但业务 schema 分层

`securefs.ValidateSegment` 只负责跨平台路径语义；业务层继续决定字段是否允许为空及命名规则：

- env/config/text managers 在所有公开入口验证 name/group/key；storage 再验证一次，防止 CLI/TUI/MCP 绕过。
- config index loader 校验 map key、`Name`、`Group` 与 `EncryptedFile`；空 `EncryptedFile` 仅内存归一化为 `<name>.enc`，非空值必须精确匹配。
- 新增 `internal/syncschema`，由 provider client 与 server store 共用 kind 白名单及 grp/key 组合矩阵。server 在开启事务/创建 vault 前验证整批；client 在 entryPath、apply 和 delete 前再次验证。
- pull 采用“先验证完整批次，再执行 apply，再提交 sync state”。验证或落盘任一步失败时 revision/state 不前进。

不把全部规则塞进 Cobra validation：manager、storage 和远端输入都不是只由 CLI 调用。

### 3. 单文件安全写是所有持久化路径的唯一入口

metadata、settings、config index、sync state、远端 apply、env/text/config 密文与 rekey sidecar 均改走 `securefs.AtomicWrite`。删除改走受根目录约束的 no-follow `Remove`；递归 group 删除先打开并验证目标目录，再相对该 handle 遍历删除，不再对字符串路径直接 `RemoveAll`。

```text
外部 identity / index / remote entry
                 │
                 ▼
       ┌───────────────────┐
       │ 业务 schema 校验  │──失败──▶ 零文件变化
       └─────────┬─────────┘
                 ▼
       ┌───────────────────┐
       │ securefs Root/FD  │  containment + no-follow
       └─────────┬─────────┘
                 ▼
      temp(0600) → write → fsync
                 │
                 ▼
          renameat → fsync(dir)
                 │
                 ▼
            完整旧值或新值
```

### 4. rekey 使用 journal + sibling generations 的可恢复提交协议

configPath 下新增私有、版本化 manifest（例如 `.senv-rekey/manifest.json`），只记录 transaction ID、阶段、规范相对身份、旧/新内容 hash 和两个根的标识；不记录口令、derived key 或明文。每个数据文件在自身目录保留同文件系统的 `<name>.rekey-<id>.new` 与提交后 `<name>.rekey-<id>.old`，避免跨文件系统 rename。

状态机：

```text
            ┌──────────┐
            │ PREPARE  │ 完整枚举/解密；写全部 .new；fsync
            └────┬─────┘
                 ▼ manifest=PREPARED + fsync
          ┌──────────────┐
          │ SWITCH_DATA  │ original→.old；.new→original；逐目录 fsync
          └──────┬───────┘
                 ▼ manifest=DATA_SWITCHED + fsync
        ┌─────────────────┐
        │ SWITCH_METADATA │ 原子写新 metadata + fsync
        └────────┬────────┘
                 ▼ manifest=COMMITTED + fsync
           ┌───────────┐
           │  CLEANUP  │ 删除 .old/.new，最后删除 manifest
           └───────────┘
```

提交点是“新 metadata 已原子持久化”。恢复先比较当前 metadata hash 与 manifest：

- metadata 仍为旧 hash：恢复所有 `.old`，删除 `.new`，回到完整旧状态。
- metadata 为新 hash：核对所有 original 为新 hash，缺失时从 `.new` 补齐，然后完成 cleanup。
- metadata 两者都不匹配、manifest 无法验证或恢复 I/O 失败：保留全部材料并锁住普通操作，提示运行 `senv doctor`/恢复新版本，不猜测处理。

所有普通 manager 构造或首次访问前调用轻量 recovery gate。rekey 持有 vault 级跨进程独占锁；普通 mutation 使用同一锁协议，避免预检后出现未纳入 manifest 的文件。仅对 rekey 自身加锁而不改普通 mutation 是不可接受的竞态。

选择 journal 恢复而非“逐个 rename 后尽力 rollback”，因为 configPath/dataPath 可能跨文件系统，无法单次原子切换整个 vault。未采用永久 generation 目录 + pointer，是为了不在本补丁改变所有长期路径和同步格式。

### 5. KDF 参数由解析结果显式返回错误

将无错误的 `EffectiveIterations()` 收敛为单一验证入口，返回 `(iterations, error)`。当前 metadata 格式规则为：缺失/0 → 100,000；显式值仅允许 `[100000, 1000000]`；JSON 溢出、负数和超界值均在 PBKDF2 前失败。所有 VerifyPassword、manager 构造、session start、rekey 共用该结果，测试通过注入 KDF 调用计数证明非法值没有进入 PBKDF2。

采用有界范围而非只允许 100k/600k，以兼容曾由外部工具创建但成本合理的 vault，并为小幅参数升级保留空间；上限仍把攻击者可施加的单次成本限制在约当前默认的 1.67 倍。

### 6. session runtime 介质按实际文件系统验证

新增 build-tagged runtime media probe：Linux 通过 `statfs` 仅接受明确的 tmpfs/ramfs 类型；其他平台只在能使用平台 API 肯定识别 memory-backed 文件系统时接受，未知类型一律拒绝。`XDG_RUNTIME_DIR` 和 `os.TempDir()` 使用同一 probe；fallback 仅在确认 memory-backed 后创建随机 0700 目录。

不提供自动磁盘降级，也不把路径名称当介质证明。其代价是默认没有 memory-backed runtime 的 macOS/容器无法创建 session；用户仍可临时输入密码，或把 `XDG_RUNTIME_DIR` 指向自行提供的内存卷。错误消息给出该配置指引，但不得建议把普通磁盘伪装成安全 runtime。

### 7. MCP 改为 request-scoped managers，不长期持有 derived key

MCP 启动时验证 session，但只保留非秘密授权指纹（session ID、dataPath hash、boot ID/timeout 类型与 metadata salt hash）。每个工具 handler 通过统一 wrapper：重新读取 cache、校验授权指纹/expiry/boot/salt/password key，临时构造 managers，执行请求后清零本次 key buffer 并释放 managers。任一校验失败先销毁可达 key，再返回 `session expired or revoked; restart senv session and MCP server`。

```text
MCP request
    │
    ▼
统一 authorization wrapper
    ├─ cache 不存在 / ID 变化 / expired / boot 变化 ─▶ 拒绝
    ├─ metadata salt 或 passwordKey 验证失败          ─▶ 拒绝
    ▼
临时 key → request-scoped managers → tool handler
    │                                  │
    └──────── defer zero/release ◀─────┘
```

不采用数百毫秒的无 I/O授权缓存，因为它会让 `session clear` 存在可观察撤销窗口；性能优化应在不跨越撤销语义的独立 change 中处理。集中 wrapper 必须覆盖全部已注册 senv tools，避免逐 handler 手写遗漏。

### 8. 明文导出统一路径解析和 mode 语义

text/config 输出先展开 `~`，再用 `filepath.Clean/Dir/Base` 解析；basename 的 parent 为当前目录，绝不手工按 `/` 切片。导出父目录以 securefs 方式创建为 0700，目标执行 no-follow 原子写。CLI mode 解析只接受 `0000`–`0777` 的八进制普通权限，拒绝特殊位。

示例：

```bash
# 默认私有
senv text get secrets:SSH_PRIVATE_KEY -o ./id_rsa
senv config export database --path ./database.yaml

# 用户明确放宽新文件权限
senv text get public:CERT -o ./cert.pem --mode 0644
senv config export public-config --path ./config.yaml --mode 0644
```

TUI 没有 mode 交互，始终使用 0600。config 覆盖既有目标时保留比请求值更严格的 mode；备份文件同样不得比原目标更宽。

## Error Handling Strategy

| 类别 | 行为 | 状态保证 |
|---|---|---|
| 非法 identity / sync schema | 返回具名但不含明文的 validation error | 文件、revision、index 不变 |
| containment / symlink / unsupported securefs | fail closed，指出安全边界无法建立 | 链接目标及根外对象不变 |
| 原子写 write/fsync/rename 失败 | 删除可确认安全的未提交临时文件；保留旧目标 | 目标为完整旧值或新值 |
| rekey PREPARE 失败 | 清理 `.new`，不切数据/metadata | 旧密码完整可用 |
| rekey 提交或恢复失败 | 保留 manifest/.old/.new，阻断普通操作 | 不暴露混合状态，不自动猜测 |
| unsafe runtime media | `session start` 非零退出并给出 memory-backed 配置指引 | derived key 不落盘 |
| MCP revoked/expired | handler 前拒绝并清零 key | 无秘密输出、无 mutation |
| KDF 参数非法 | 在 PBKDF2 前返回 metadata/KDF 错误 | 无高成本计算，不误报密码错 |

错误包装保留 `errors.Is/As` 可判定类型；日志和 CLI 可包含 kind/group/key 或相对文件身份，但不得记录明文、derived key、session cache 内容或完整 Bearer token。

## Risks / Trade-offs

- **[平台文件 API 差异]** `openat2`、`openat`、目录 fsync 和 rename 语义不完全一致 → 用 build-tag backend 和契约测试；不能证明等价的平台明确 fail closed，而不是退回普通路径 API。
- **[session 可用性回退]** macOS、容器或自定义 `XDG_RUNTIME_DIR` 可能没有可确认的 memory-backed FS → 错误中提供内存卷配置方法；临时密码仍可用；不以默认磁盘 fallback 换取便利。
- **[rekey 状态机复杂度]** journal 自身损坏或磁盘满可能阻断 vault → manifest 原子写、版本/hash 校验、每阶段故障注入与子进程 crash matrix；恢复失败保留材料。
- **[锁覆盖遗漏]** 某个 mutation 若不使用 vault 锁，会破坏 rekey 快照 → 将锁放在 storage mutation 公共边界，并用并发测试覆盖 env/text/config/sync apply。
- **[严格名称校验暴露历史坏数据]** 旧的非法 group/index 将从“可能工作”变为明确错误 → 只读诊断列出身份，不自动跟随；提供恢复说明，不自动删除。
- **[导出兼容变化]** 默认 0600、0700 父目录或拒绝 symlink 可能影响依赖共享文件的脚本 → `--mode 0644` 显式迁移；release note 标为 breaking；不持久化宽松默认。
- **[额外 fsync/I/O]** 安全写和 rekey 比现状慢 → 这是低频安全路径；先保证 durability，性能优化另行 benchmark。

## Migration Plan

1. **先交付 Stage 0**：加入 securefs 最小原语、vault mutation lock、rekey journal/recovery 与故障注入。发布前禁止继续宣称旧 rekey 的失败回滚可靠；新版本启动先执行 recovery gate。
2. **再交付 Stage A**：server 先部署共享 sync schema 验证（旧客户端产生的合法条目兼容），随后发布客户端的 manager/storage 双层校验、secure write/delete 和私密导出默认值。
3. **最后交付 Stage B**：启用 KDF 上限、runtime media probe 与 MCP request guard；同步更新 README/SECURITY 的 600k/legacy 与 session 平台限制说明。
4. 长期 env/text/config 密文和 index/metadata JSON 格式不变。rekey manifest 与 sidecar 仅在事务期间存在；legacy 空 `EncryptedFile` 只做内存归一化。
5. 回滚二进制前必须让新版本完成或恢复任何 active rekey，并确认 manifest 不存在。正常完成后可回滚，因为主数据格式未变；不得用旧版本直接处理未完成 journal。
6. server 端严格验证不改变 API shape，可独立回滚；客户端仍保留本地验证。若发布后出现平台 securefs/runtime 不支持，只能回滚到已知版本或补充经审计 backend，不得用普通写/磁盘 session 临时旁路。

## Open Questions

无。平台 backend 的具体系统调用封装可在不改变上述安全契约、规格和任务拆分的前提下实现与审计。
