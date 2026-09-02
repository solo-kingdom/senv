## 1. HTTP client 与状态

- [x] 1.1 实现 senv-server v1 API client（metadata 读写、push、pull、错误解析含 409 冲突清单）
- [x] 1.2 本地同步状态文件（last_synced_revision、dirty 标记）读写

## 2. provider 实现

- [x] 2.1 server provider 接入窄接口：Pull（增量落盘）/ Push（dirty 收集 + 批量提交）/ Sync / Status
- [x] 2.2 写路径统一打 dirty 标记；冲突时列出条目并给出解决指引，不改任一端数据

## 3. CLI 接入

- [x] 3.1 `senv init --server`：拉取 metadata 建缓存 + vault 口令解锁（口令不发 server）
- [x] 3.2 sync 命令 server 模式分支；离线时读写不受限、同步报网络错误
- [x] 3.3 session 缓存在 server 模式下复用现有机制

## 4. 验证

- [x] 4.1 同步引擎单测（增量、冲突、离线恢复）；与 server 子 change 的端到端联调
- [x] 4.2 `make check` 全绿；`openspec validate --strict --type change senv-server-provider-client` 通过
