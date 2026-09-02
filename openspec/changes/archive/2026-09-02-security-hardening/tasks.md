## 1. crypto:KDF 参数化(基础件,先行)

- [x] 1.1 【高优】`internal/crypto/keyderive.go` 新增 `DeriveKeyWithIterations(password, salt, iterations)`;`DeriveKey` 保留并委托为 100k 缺省。验证:单测断言两种派生结果关系与输出长度 256 位
- [x] 1.2 【高优】`internal/storage/types.go` 的 `Metadata` 增加 `KDFIterations int json:"kdf_iterations,omitempty"` 与 `EffectiveIterations()`(0 → 100000)。验证:单测覆盖新旧 JSON 往返(缺字段按 100k,显式值保留)

## 2. storage:派生路径透传 + 敏感文件权限 helper

- [x] 2.1 【高优】storage/env/session 各派生调用点改为按 metadata 的有效迭代次数派生(storage/manager.go 全部解锁点、internal/env/manager.go:66、internal/session/manager.go:55)。验证:新建 600k vault 可解锁读写;手工构造 100k metadata 的旧 vault 仍可解锁(desync 探针通过)
- [x] 2.2 【高优】`senv init` 与 metadata 生成路径写入 `kdf_iterations: 600000`。验证:新 init 的 metadata.json 含该字段;`senv doctor`/解锁正常
- [x] 2.3 【高优】`senv passwd`(cmd/passwd.go)以 600k 派生新钥,rekey 完成后 metadata 写 600000;输出提示"旧版客户端将无法解锁"。验证:对既有测试 vault 执行 passwd 后,全量数据可解、metadata 字段更新、失败回滚用例通过
- [x] 2.4 【高优】新增 `WriteSensitiveFile(path, data, dirPerm, filePerm)` helper:MkdirAll(0700) → 存量文件/目录宽于目标权限则先 chmod → WriteFile。验证:单测覆盖新建、0644 存量收紧、0700 目录收紧三场景
- [x] 2.5 【高优】`SaveSettings`(internal/storage/manager.go:266)及其余敏感落盘点改走 helper。验证:单测中断言覆写后权限为 0600

## 3. session:tmpfs-only + 创建加固

- [x] 3.1 【高优】删除 `getPersistentCachePath` 持久化路径,所有模式缓存仅写 `XDG_RUNTIME_DIR`;`getCachePathForType` 收敛为单一路径;写缓存时删除 `~/.local/share/senv/session/` 遗留文件。验证:单测断言 never 模式不产生持久文件且遗留文件被清理
- [x] 3.2 【高优】回退路径改为 `os.MkdirTemp("/tmp", "senv-session-")` + chmod 0700,文件 `O_CREATE|O_EXCL|0600` 写入,拒绝符号链接。验证:单测用 temp 目录模拟无 XDG_RUNTIME_DIR 环境,断言目录随机、二次写不覆盖已存在文件
- [x] 3.3 【高优】`generateSessionID`(session/cache.go)与 `rand.Read` 错误上抛,`session start` 失败即中止、不落盘。验证:注入随机数失败(fault 注入或接口桩)断言返回错误且无缓存文件
- [x] 3.4 `session status`/start 输出补 never 语义提示("cleared on logout/reboot")。验证:命令输出快照断言

## 4. provider:scheme 校验

- [x] 4.1 【高优】`provider.New`(internal/provider/provider.go)对 server provider 校验地址 scheme:非 https 且未设 `SENV_ALLOW_INSECURE_HTTP=1` 时返回含地址、原因与豁免方法的错误,不发起请求。验证:表驱动单测(https/http+豁免/http 无豁免/非法 URL)
- [x] 4.2 集成验证:配 http 地址执行 `senv env list` 得到可读错误;设豁免变量后走通本地 mock server。验证:e2e 或手工验证记录

## 5. server:超时、体积、限速、脱敏、字段校验

- [x] 5.1 【高优】`senv-server/main.go` 换显式 `http.Server`(ReadHeaderTimeout 10s / ReadTimeout 120s / WriteTimeout 120s / IdleTimeout 120s / MaxHeaderBytes 1MB)。验证:单测或 httptest 断言配置注入;慢头连接集成用例
- [x] 5.2 【高优】`maxBodySize` 改 flag `-max-body-bytes`(默认 64MB),handler 读配置。验证:单测覆盖默认值与超限 413
- [x] 5.3 【高优】认证失败固定窗口限速(默认 30 次/分钟/IP,阈值 flag),超限 429、响应与 401 同构不泄露存在性;成功认证不计数。验证:单测模拟连续失败与成功路径
- [x] 5.4 【高优】错误分级:store 校验类错误(新增可判别类型)维持 400 带原因;其余错误写服务端日志、统一 500 `"internal error"`。验证:单测断言内部错误响应体不含 SQL/驱动细节、日志含完整错误
- [x] 5.5 kind/grp/key 长度上限校验(对齐 internal/storage/validate.go 常量),落库前返回 400 指明字段。验证:单测覆盖三字段超限与边界值
- [x] 5.6 回归:`senv-server` e2e(push/pull/冲突/增量拉取)全绿,确认协议未变。验证:`make test` + 既有 e2e 套件

## 6. 文档与部署 runbook

- [x] 6.1 更新 docs/SECURITY.md:迭代次数 100k→600k(新 vault/rekey)、版本化说明、降级限制(600k vault 需新版客户端)、never 会话 tmpfs 语义。验证:文档评审,与实现行为一致
- [x] 6.2 【高优】docs/senv-server.md 新增"加固清单"runbook:registry htpasswd + tcbj 只读凭据配置、PG `REVOKE CONNECT ON DATABASE casdoor, senv FROM PUBLIC` 及验证 SQL、存量文件一次性 `chmod -R go-rwx ~/.config/senv ~/.local/share/senv`、备份排除项、caddy 可选限速片段、`-max-body-bytes`/限速阈值调参说明。验证:按 runbook 在测试环境演练一遍命令
- [x] 6.3 迁移说明:存量用户升级后首次运行需重新解锁一次;`senv passwd` 升级 KDF 的操作指引。验证:文档评审

## 7. 收尾

- [x] 7.1 全量 `make check`(fmt + vet + lint + test -race)通过。验证:CI/本地输出
- [x] 7.2 本机实测验收:覆写存量 0644 settings.json 后变 0600;`~/.local/share/senv/session/` 遗留文件被清理;never 会话位于 XDG_RUNTIME_DIR。验证:命令输出与 ls -l 记录
