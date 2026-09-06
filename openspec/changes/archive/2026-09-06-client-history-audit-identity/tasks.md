## 1. Server 存储与迁移（安全：高优先级）

- [x] 1.1 编写 0002 迁移：clients、registration_codes 表与 tokens.client_id 可空列（含 UNIQUE 约束与索引）；验证：`senv-server migrate` 在临时库执行通过且 0001 数据保留
- [x] 1.2 store 层实现 client 记录、注册码签发/哈希/原子核销、按 token 解析 client；配对单测：注册码一次性、过期、重放拒绝（验证：go test 通过）
- [x] 1.3 store 层实现屏蔽/解封与 list-clients 查询；配对单测：状态流转、同 user 其他 client 与 vault 数据不受影响（验证：go test 通过）

## 2. Server API 与 admin CLI（安全：高优先级）

- [x] 2.1 认证中间件接入 client 状态判定：blocked → 403 `{"error":"client_blocked"}`，无效/缺失/吊销仍 401；配对单测覆盖两类响应与「未知 token 不泄露屏蔽语义」（验证：go test 通过）
- [x] 2.2 实现 POST /v1/register（限速保护、明文 token 仅返回一次、设备名冲突 409）；配对单测：成功/无效码/过期/重放/冲突（验证：go test 通过）
- [ ] 2.3 admin 子命令 create-registration / list-clients / block-client / unblock-client；验证：手工执行命令核对输出、退出码与库内状态

## 3. Client 注册与感知（安全：高优先级）

- [x] 3.1 server_client.go 将 403+`client_blocked` 映射为 BlockedError，非该语义的 403 不触发清理；配对单测覆盖映射分支（验证：go test 通过）
- [x] 3.2 命令层捕获 BlockedError：清 SessionCache、写审计事件、输出屏蔽提示与重新注册指引、非零退出；本地加密数据不动；配对单测验证清理调用与文件保留（验证：go test 通过）
- [x] 3.3 新增 `senv server register` 命令（地址 scheme 校验复用 provider 规则、token 0600 原子写入 settings.json）；配对单测覆盖成功与失败路径（验证：go test 通过）

## 4. 回归与闭环验证

- [x] 4.1 `make check` 全绿；既有 server-auth / server-sync 相关测试回归通过（验证：记录命令输出）
- [x] 4.2 双端手工闭环：create-registration → register → 同步 → block-client → client 感知并清缓存 → unblock-client 恢复；验证：结果写入 proposal 验证记录
