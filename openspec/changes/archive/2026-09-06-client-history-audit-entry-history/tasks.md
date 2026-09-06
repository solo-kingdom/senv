## 1. Server 存储与迁移

- [x] 1.1 编写 0003 迁移：entries_history 表（vault_id/kind/grp/key/ciphertext/revision/created_at，含按条目与时间的索引）；验证：`senv-server migrate` 临时库执行通过
- [x] 1.2 push 事务内实现前像留存与按 N 裁剪（N 来自 --history-retain，默认 3，≤0 关闭）；配对单测：修改/删除均产生历史、版本数不超 N、关闭时不写历史（验证：go test 通过）
- [x] 1.3 store 层实现历史查询（按条目、vault 级时间倒序+分页）；配对单测：排序、分页、跨用户 404（验证：go test 通过）

## 2. Server API

- [x] 2.1 实现 GET /v1/vaults/{vault}/history（认证、vault 隔离、kind/grp/key 过滤与 limit）；配对单测覆盖过滤与隔离（验证：go test 通过）
- [x] 2.2 server 启动参数 --history-retain 接入与默认值校验；验证：手工启动核对生效

## 3. Client 查看与恢复

- [x] 3.1 provider 层新增历史拉取与解密（复用现有鉴权与错误映射；解密失败版本跳过并标注）；配对单测：列表解析、解密失败降级（验证：go test 通过）
- [x] 3.2 新增 `senv history` 命令：vault 最近变更与单条目版本列表（含日期时间、与当前值对比、无历史友好提示）；配对单测覆盖输出分支（验证：go test 通过）
- [x] 3.3 TUI 新增历史视图（版本时间线、选中版本内容、恢复入口）；验证：手工浏览核对渲染与翻页
- [x] 3.4 实现单条目恢复（含已删除条目找回）：交互确认 → 本地写回 → 既有推送；配对单测：恢复产生新 revision、冲突时走既有冲突流程（验证：go test 通过）

## 4. 回归与闭环验证

- [x] 4.1 `make check` 全绿；既有 server-api / server-sync 测试回归通过（验证：记录命令输出）
- [x] 4.2 手工闭环：改条目 3 次以上 → history 查看（含日期）→ 恢复旧版 → 删除条目 → 从历史找回；验证：结果写入 proposal 验证记录
