## Why

安全审查(源码 + tcbj 部署)发现 3 个 P1、6 个 P2:会话派生钥明文持久化(本机当前即成立)、敏感文件权限不收紧(server token 将落入 0644 settings.json)、registry 无认证可任意 push;另有 client 不校验 https、PBKDF2 轮数偏低、server 零超时与 600MB body、认证无限速、PG 信任边界过宽、内部错误透传。本次集中收敛。

## What Changes

- session:**BREAKING** `never` 模式不再把派生钥明文写入 `~/.local/share/senv/session`,所有模式仅驻留 tmpfs(XDG_RUNTIME_DIR);`/tmp` 回退改随机目录 + `O_EXCL`;`rand.Read` 错误不再忽略
- 文件权限:敏感文件(settings.json、session 缓存、凭据)写入路径强制 0600/0700,重写已存在的宽松权限文件前先收紧
- crypto:metadata 增加 `kdf_iterations`(缺省 100k 保持兼容),新 vault 与 `senv passwd` rekey 后使用 600k
- provider:**BREAKING** server address 默认仅接受 https,http 需显式环境变量豁免
- server:显式 `http.Server` 超时;body 上限 600MB→64MB;认证失败按源限速;错误响应改通用消息(细节进服务端日志);kind/grp/key 长度校验
- 部署文档(docs/senv-server.md、SECURITY.md):registry 加 htpasswd、PG `REVOKE CONNECT FROM PUBLIC`、备份排除 session 目录与一次性 `chmod go-rwx` 的运维 runbook

## Non-goals

- 系统 keyring 集成(headless 可用性差;never 转 tmpfs 已消除主要风险)
- Argon2id 迁移(先提轮数至 600k,格式大版本演进另议)
- 拆分共享 PG 实例 / casdoor 降权(远期架构项)
- server 访问审计日志、内存密钥清零(已记录,另行处理)
- 在 tcbj 实际执行运维操作(本 change 只产出 runbook,执行走运维窗口)

## Capabilities

### New Capabilities
- `crypto-kdf`:KDF 迭代参数版本化、旧格式兼容与 rekey 升级路径
- `sensitive-file-permissions`:本地敏感文件创建与重写时的权限保证

### Modified Capabilities
- `session-auth`:never 模式缓存位置变更与缓存文件加固
- `provider-abstraction`:统一构造入口增加 server address scheme 校验
- `server-auth`:新增认证失败限速要求
- `server-api`:新增请求超时/体积上限、错误响应脱敏、字段长度校验

## Impact

- 代码:internal/session、internal/storage(types/rekey/manager)、internal/crypto、internal/provider、internal/server/{handler,store}、senv-server/main.go
- 行为变更:`never` 会话不再跨重启;非 https 地址默认拒绝;超过 64MB 的请求被拒
- 兼容:旧 metadata(无 kdf 字段)仍可读,经 `senv passwd` 平滑升级
- 部署:registry/PG/caddy 配置按 runbook 变更,不影响 senv-server 协议

## 安全性分析

- 持久磁盘上不再存在可直接解库的明文派生钥,PBKDF2 防线恢复意义;离线爆破单次成本由 100k 提至 600k 轮
- 权限收紧阻断同机其他用户与备份链路读取 token/缓存;scheme 校验防止凭据与密文明文传输
- server 超时/限速/脱敏降低 DoS 与信息泄露面;零知识协议与加密格式本体不变,AES-256-GCM、盐/nonce 长度均维持现状
