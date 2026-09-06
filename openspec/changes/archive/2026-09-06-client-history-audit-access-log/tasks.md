## 1. 存储与迁移（安全：高优先级）

- [x] 1.1 编写 0004 迁移：access_log 表与索引；验证：`senv-server migrate` 临时库执行通过
- [x] 1.2 store 层实现事件写入、过滤查询与分批清理；配对单测：写入字段完整、查询过滤（用户/client/日期/结果/条数）、清理只删该日期之前（验证：go test 通过）

## 2. 中间件与结果标注（安全：高优先级）

- [x] 2.1 实现 access-log 中间件与 response wrapper：healthz 跳过、认证失败原因经 context 传递、429 前置拦截也留痕；配对单测：OK/AUTH-FAILED/BLOCKED/RATE-LIMITED 四类结果各产生正确事件（验证：go test 通过）
- [x] 2.2 验证记录不含 token/口令/密文（对日志行断言）；配对单测覆盖（验证：go test 通过）

## 3. admin CLI

- [x] 3.1 实现 `senv-server admin logs` 查询（过滤参数、含日期时间的表格输出）；验证：手工执行核对输出与退出码
- [x] 3.2 实现 `senv-server admin logs-prune --before`（分批删除）；验证：手工执行核对删除范围

## 4. 回归与闭环验证

- [x] 4.1 `make check` 全绿；既有 handler/store 测试回归通过（验证：记录命令输出）
- [x] 4.2 手工闭环：正常访问、错 token、屏蔽 client、连续失败触发 429、push/pull 同步 → admin logs 各场景可查（含日期）→ logs-prune 清理旧事件；验证：结果写入 proposal 验证记录
