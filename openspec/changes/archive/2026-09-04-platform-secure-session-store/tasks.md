## 1. SessionStore 抽象与 symlink 修复

- [x] 1.1 【高优】在 `internal/session` 定义 `SessionStore` 接口（Save/Load/Clear），将现有 tmpfs/XDG/fallback 逻辑收敛为 `tmpfsStore`，`saveCache/loadCache/clearCache` 改为委托调用，不改变任何加固属性。验证：`make test` 全绿且现有 session 单测无需修改即通过。
- [x] 1.2 【高优】修复 runtime root 校验：先对候选路径做可信符号链接解析，再对解析后路径做组件校验与介质探测；写入仍走 `securefs` no-follow 锚定。验证：新增单测覆盖"系统 symlink 解析后放行"与"用户可控 symlink 拒绝"两个分支，`go test ./internal/session -race` 通过。

## 2. darwin Keychain 后端

- [x] 2.1 【高优】实现 `keychainStore`：经 `/usr/bin/security` add/find/delete-generic-password 访问 `senv.session.<uid>` item，写入时 `-T /usr/bin/security`，payload 复用现有 `SessionCache` JSON。验证：用可注入的 exec seam 编写单测覆盖写入、读取、不存在、`security` 失败四类路径；`GOOS=darwin go build ./...` 通过。
- [x] 2.2 【高优】实现平台选择：darwin 默认 keychainStore，linux 默认 tmpfsStore；Keychain 不可用时返回含逃生舱指引的结构化错误。验证：单测断言各平台默认后端与错误文案，`go test ./internal/session -race` 通过。

## 3. 磁盘逃生舱

- [x] 3.1 【高优】实现 `diskCacheStore`：`${XDG_CACHE_HOME:-~/.cache}/senv/session.json`，0600 原子写，读取顺序为平台存储 → 逃生舱，`session clear` 清理两者，双有效缓存报 "multiple session caches"。验证：单测覆盖权限、原子替换、boot ID 失效、双缓存冲突与 clear-all；`go test ./internal/session -race` 通过。
- [x] 3.2 【高优】为 `session start` 增加 `--insecure-cache` 开关：开启前向 stderr 输出醒目安全警告，默认关闭。验证：单测断言默认拒绝写盘、开启后警告与写入行为；手动运行命令核对输出。

## 4. 错误信息与 MCP 回归

- [x] 4.1 【高优】统一缓存不可用错误为可行动指引（Keychain 锁定、介质不合格、security 缺失时说明原因与 `--insecure-cache` 用法）。验证：单测断言各错误分支文案包含原因与行动项。
- [x] 4.2 【高优】用 fake SessionStore 回归 `AuthorizeMCPRequest`：连续多次请求均重新加载并校验，全程零交互，撤销/过期行为不变。验证：`go test -race ./internal/session -run 'MCP|Authorize'` 通过。

## 5. 文档与验收

- [x] 5.1 更新 README、SECURITY、RELEASE_NOTES：平台安全存储矩阵（Linux tmpfs / macOS Keychain）、Keychain 安全定位与限制、逃生舱警告。验证：文档审阅，无与 spec 冲突表述。
- [ ] 5.2 【高优】macOS 真机验收：start/status/get/clear 全链路、MCP 连续请求零弹窗、重启后 never 失效、headless 下输出逃生舱指引。验证：记录实际命令输出与 Keychain item 状态。
- [x] 5.3 Linux 回归：XDG tmpfs 正常、disk-backed XDG 拒绝、逃生舱写/读/清。验证：`make check` 通过并手动跑完三场景矩阵。
