# Design

## Context

senv 的零知识与加密本体(AES-256-GCM、32 字节盐、12 字节 nonce)经审查确认无需改动;问题集中在密钥的"周边存放与传输":派生钥明文持久化、文件权限、KDF 强度、传输层与 server 韧性。所有代码改动为防御深度,不改变同步协议与加密格式本体(仅 metadata 增加一个字段)。

## Goals / Non-Goals

见 proposal。设计层补充边界:

- 不引入新的第三方依赖(限速、权限、O_EXCL 均用标准库实现)
- 不改变 client/server HTTP 协议与 schema 版本
- 不追求跨平台 keyring 抽象

## Decisions

### D1. never 会话改为 tmpfs-only,而非接入系统 keyring

备选:(a) go-keyring 集成 —— Linux 依赖 Secret Service/dbus,headless 环境不可用,失败路径复杂;(b) 维持持久化仅加警告 —— 未消除"密钥与密文同机同权且被备份带走"的核心风险。选择 tmpfs-only:`never` 语义收敛为"不设时间过期,登出/重启即失效"(与 `restart` 的差别仅剩不做 BootID 校验)。实现上删除 `getPersistentCachePath`,`getCachePathForType` 恒返回 tmpfs 路径;写缓存时顺带删除 `~/.local/share/senv/session/` 遗留文件;`session status` 文案补"登出/重启后失效"。

### D2. 缓存文件用随机私有目录 + O_EXCL,而非修符号链接检查

回退路径不再用可预测的 `/tmp/senv-session-<uid>`,而是 `os.MkdirTemp("/tmp", "senv-session-")` 后 chmod 0700,文件以 `os.OpenFile(O_WRONLY|O_CREATE|O_EXCL, 0600)` 写入,从根上消除抢占/替换窗口。`generateSessionID` 与 `rand.Read` 错误一律上抛,`session start` 失败退出、不落盘(fail-closed)。

### D3. 权限收紧:统一写入 helper,写前 chmod 而非写后

`os.WriteFile` 不改已存在文件权限,写后 chmod 又存在瞬时窗口,故选写前 `os.Chmod`(Stat 宽于目标权限才调)。在 `internal/storage` 增加 `WriteSensitiveFile(path, data, dirPerm, filePerm)` helper:`MkdirAll(dir, 0700)` → 存量文件/目录收紧 → `WriteFile(0600)`。接入点:`SaveSettings`(internal/storage/manager.go:266)、session `saveCache`、server provider 凭据相关写路径。

### D4. KDF 参数:metadata 增加 `kdf_iterations`,100k 为隐式缺省

`Metadata` 增加 `KDFIterations int \`json:"kdf_iterations,omitempty"\``;新增 `crypto.DeriveKeyWithIterations(password, salt, n)`,`DeriveKey` 保留为 100k(兼容调用)。有效迭代次数 = `max(md.KDFIterations, 1)`,为 0 按 100k。派生调用方(storage manager 各解锁点、env/manager.go:66、session/manager.go:55)从 metadata 取参数 —— 这些路径已持有 metadata,改动为传参透传。`init` 写 600000;`senv passwd`(cmd/passwd.go:66 调 `Manager.Rekey`)以新参数派生新钥并在 metadata 写 600000,Rekey 已有全量重加密与回滚,无需新迁移代码。服务器零知识存储 blob,字段增加对 server 透明。

### D5. https 校验放在 provider 构造入口,环境变量豁免

`provider.New`(internal/provider/provider.go)对 server provider `url.Parse` 校验 scheme:`https` 通过;`http` 仅当 `SENV_ALLOW_INSECURE_HTTP=1` 时通过,否则报错,错误信息含地址与豁免方法(复用"统一构造入口"的错误可读性要求)。选环境变量而非 settings 字段:豁免是机器级决策,且避免把"允许明文"固化进可同步/可备份的配置。git provider 不校验。

### D6. server:显式 http.Server + 限速 + 错误分级,均不引入依赖

- `senv-server/main.go` 的 `http.ListenAndServe` 换为 `&http.Server{ReadHeaderTimeout: 10s, ReadTimeout: 120s, WriteTimeout: 120s, IdleTimeout: 120s, MaxHeaderBytes: 1MB}`;64MB 对慢链路上传约需 >1 分钟,ReadTimeout 取 120s。
- `maxBodySize` 改为 flag `-max-body-bytes`(默认 64MB),运维可按需上调。
- 限速:`sync.Mutex` 保护的 `map[ip]window{count, resetAt}` 固定窗口(默认 30 次失败/分钟),仅统计 401 路径,超限 429;单实例部署内存态足够,重启清零可接受。
- 错误分级:store 校验类错误(参数/长度/格式)维持 400 + 具体原因;其余错误记服务端日志(含来源与路径),统一返回 500 `"internal error"`。在 handler 内以错误类型区分,store 层新增可判别的校验错误类型。
- 字段长度:server 侧常量对齐客户端 `internal/storage/validate.go` 既有上限,落库前校验,超限 400 指明字段。

## 数据流(加固点标注 ★)

```
解锁/解密(client)
  password ──PBKDF2(salt, kdf_iterations★)──▶ derived key ──▶ AES-256-GCM 开密文
                                                  │
                        session cache(仅 tmpfs★,O_EXCL+0600★,random dir★)
  metadata.json: {salt, password_key, kdf_iterations★}   (0600 写入保证★)

同步(client → server)
  provider.New:scheme 校验★(https 默认,SENV_ALLOW_INSECURE_HTTP 豁免)
  Bearer token ──▶ server:固定窗口限速★ → 401/429(不泄露存在性)
  push/pull body:MaxBytesReader(64MB 默认★)→ 超时受 http.Server 约束★
  server 落库前:kind/grp/key 长度校验★;非校验类错误 → 日志+通用 500★
```

## 错误处理策略

- client 一律 fail-closed:随机数失败、缓存写失败、scheme 违规均中止且不落敏感状态;错误信息含"下一步怎么做"(如豁免方法、`senv passwd` 升级提示)。
- server 将错误分为"客户端可理解"(校验类,400 带原因)与"内部"(日志留痕,500 通用消息);认证失败统一 401/429 不区分原因,防枚举。
- Rekey 沿用既有回滚:重加密失败恢复原文件,metadata 最后提交。

## 向后兼容与存储格式变更

- metadata 新字段 `omitempty`,旧客户端→新服务端、新客户端→旧服务端的 blob 均可解析(服务器不解释 blob)。
- 旧 vault(无字段)按 100k 解锁,行为不变;升级到 600k 仅发生在用户显式 `senv passwd`。
- **降级限制**:rekey 升级到 600k 后,旧版二进制(硬编码 100k)将无法解锁 —— 因 passwordKey 校验必然失败。在 `senv passwd` 输出与 SECURITY.md 明示"升级后需使用 ≥ 本次版本号的客户端"。
- never 会话变更对旧版本不对称:新版本清理遗留持久缓存;若回退旧版本再 start,会重新写持久缓存(已记录于迁移说明)。

## Migration Plan

1. client:发布新二进制 → 用户升级后首次运行清理遗留持久缓存并要求重新解锁一次(一次性输口令)。
2. server:部署新二进制(无 schema 变更、协议不变),caddy 前置不变;回滚即换回旧二进制。
3. KDF 升级由用户按需执行 `senv passwd`;文档给出"升级前确认所有在用机器均已升级客户端"的清单。
4. 部署侧 runbook(docs/senv-server.md 新增"加固清单"):registry htpasswd + tcbj 只读凭据、PG `REVOKE CONNECT ON DATABASE casdoor, senv FROM PUBLIC`、存量文件一次性 `chmod -R go-rwx ~/.config/senv ~/.local/share/senv`、备份排除项更新。

## CLI / 用户可见变化示例

```console
# never 会话:不再跨重启,重启/登出后需重新解锁
$ senv session start --timeout never
Session started (never expires; cleared on logout/reboot)

# 内网 http 豁免
$ SENV_ALLOW_INSECURE_HTTP=1 senv config set provider.address http://10.0.0.5:8080

# KDF 升级(同时改口令;若保持口令,输入两次相同新口令)
$ senv passwd
```

## Risks / Trade-offs

- [never 语义变化造成体验回退(重启需重新输口令)] → 属安全-便利的显式取舍,文档与提示语明示;keyring 记为后续评估项
- [rekey 后旧客户端不可解锁] → `senv passwd` 提示 + SECURITY.md 降级限制说明;Rekey 保留回滚
- [600k 轮使解锁/会话派生耗时约 ×6(数百 ms)] → 单次解锁可接受;rekey 一次性成本
- [限速按 IP,内网 NAT 后多用户共享窗口] → 阈值仅约束失败请求,正常使用不触发;阈值做成 flag
- [写前 chmod 与写入之间仍有微小竞态] → 单用户场景可接受;helper 内先收紧再写,窗口远小于现状(永不收紧)
- [错误统一 500 降低排障信息量] → 服务端日志保留完整错误,响应附请求时间戳便于对日志

## Open Questions

- 限速阈值(30/分钟)与 body 上限默认值(64MB)先按 design 取值落地,做成 flag 后由 tcbj 实际运行数据再调。
