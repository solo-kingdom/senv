## 1. 存储层

- [x] 1.1 【安全/高优先级】在 `internal/storage` 新增 `HostEntry`/`KeyPairEntry` 类型与 `dataPath/hosts`、`dataPath/keypairs` 读写路径，复用 secure_write 与文件锁。验证：单元测试覆盖加密写入→读取往返、原子写、并发写锁
- [x] 1.2 验证 `doctor`/repair/一致性检查对新增顶级目录的容忍度；若误报则实现 quarantine 处理。验证：构造含 hosts/keypairs 的 vault 运行 doctor 无误报

## 2. keypair 管理层

- [x] 2.1 新增 `internal/ssh` keypair 导入：读取文件→格式解析校验→公钥派生（`ParseRawPrivateKey`→`MarshalAuthorizedKey`）→加密存储；重名默认拒绝（`--force` 覆盖）。验证：单元测试覆盖 ed25519/rsa 派生、加密私钥公钥留空、文件不存在、重名拒绝
- [x] 2.2 实现 keypair `list`（指纹/公钥标注 `pubkey: none`）与 `delete`（引用扫描、默认拒绝并列引用者、`--force` 清空引用后删除）。验证：单元测试覆盖删除保护与引用清空
- [x] 2.3 实现 `senv keypair import/list/delete` cobra 命令，操作写入 Operation Audit。验证：集成测试走 CLI 全流程断言输出与审计记录
- [x] 2.4 【安全/高优先级】实现 `senv keypair materialize`：落盘 `~/.ssh/senv/<name>`，目录 0700、文件 0600、已存在默认拒绝。验证：单元测试断言创建的目录/文件权限位与覆盖拒绝行为

## 3. host 管理层

- [x] 3.1 实现 `internal/ssh` host CRUD 与写路径校验：alias 全局唯一、`identityKey` 存在性、`proxyJump` 存在性、`extra` 键非空且不含空白。验证：单元测试覆盖各校验分支
- [x] 3.2 实现 `host add` 三通道密钥联动：`--keypair`、`--key-file`+`--keypair-name` 一步导入关联、无参数时交互列出可选并可跳过。验证：集成测试覆盖三通道与无效引用拒绝
- [x] 3.3 实现 `senv host add/get/edit/list/delete` 命令，`edit` 复用编辑器流程，操作写入 Operation Audit。验证：集成测试覆盖 CLI 全流程

## 4. 导出联动

- [x] 4.1 实现 `senv host export`：核心字段映射、extra KV 直传、`IdentityFile` 指向 `~/.ssh/senv/<name>`、`--host` 过滤、悬空 `proxyJump` 报错。验证：golden 文件测试比对渲染输出
- [x] 4.2 端到端联调：import → `host add --key-file` → materialize → export → ssh -G 解析验证片段合法。验证：集成测试或手工验证记录（`ssh -G` 无报错）

## 5. TUI 与 MCP

- [x] 5.1 TUI 新增 hosts/keypairs 浏览板块，keypair 显示指纹/公钥摘要、私钥内容遮蔽。验证：沿用现有 TUI 测试模式断言渲染不含明文
- [x] 5.2 【安全/高优先级】注册 MCP 只读工具 `ssh_host_list`/`ssh_host_get`，返回字段白名单；不注册任何返回私钥明文的工具。验证：扩展 MCP guard 测试断言工具清单与返回字段不含私钥材料

## 6. 收尾

- [x] 6.1 更新 README 功能特性与安全提示（materialize 文件常驻磁盘、extra KV 直传风险、`Include` 用法示例）。验证：文档自查与示例命令可复制执行
- [x] 6.2 全量回归：`make check` 通过，`openspec validate --strict` 通过。验证：CI/本地命令输出无错误
