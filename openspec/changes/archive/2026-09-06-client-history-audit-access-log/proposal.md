## Why
server 无法回答「谁在何时尝试访问、为何失败」，缺乏安全审计依据。需将每个 API 请求与认证结果落库，供服务器管理员追溯登录成功/失败、屏蔽拦截、限速与同步行为。

## What Changes
- server 新增 `access_log` 表（0004 迁移）：时间、来源 IP、method+path、可解析时的 client/user 标识、结果（OK/AUTH-FAILED/BLOCKED/RATE-LIMITED）与失败原因
- HTTP 中间件统一记录（healthz 除外）；同步事件即 push/pull 请求，无需单独埋点
- admin CLI 新增 logs 查询（--user --client --since --until --outcome --limit）与 logs-prune（--before 日期）
- 不向 client 开放查询 API

## Non-goals
- client 远程查询；自动保留/轮转策略（仅手动 prune）；stdout 日志改造；日志导出

## Capabilities

### New Capabilities
- `access-log`: server 每请求安全事件落库、管理员查询与清理

### Modified Capabilities
（无——既有 API 行为不变，日志为旁路记录）

## Impact
- server：internal/server/migrate（0004）、internal/server/handler（中间件）、internal/server/store（写入/查询/清理）、senv-server/main.go（admin logs 子命令）
- 依赖：client-history-audit-identity 的 clients 表（有 client 归属记 client 标识；存量 user 级 token 记 user 标识）
- 隐私：不含 token、口令与密文内容

## 安全性分析
- 日志仅含连接与身份元数据，不含凭证材料与密文
- 写入 best-effort，日志故障不阻断服务
- 查询与清理仅限服务器本机 admin CLI，不暴露网络端点
- 429 限速拦截同样留痕，利于发现爆破尝试

## 验证记录
- 2026-09-06：`make check` 全绿，含新增 store/handler 访问日志测试（四类结果、healthz 跳过、token 不落日志、过滤与分批清理）。
- 2026-09-06：闭环验证以真实 Postgres + 二进制冒烟承担：migrate（0004）→ serve → healthz（不记录）/ 有效凭证（OK，含 user 名）/ 坏 token（AUTH-FAILED + 原因）→ `admin logs`（含日期表格）→ `--outcome AUTH-FAILED` 过滤 → `logs-prune --before` 删除 → 复查为空。
- 实现要点：身份/拦截原因经可变 `accessInfo` 指针在请求 context 上原地传递（内层中间件写入、外层包装器读取）；`RecordAccess` 零值时间默认 now()；客户端不开放任何查询端点（spec 场景覆盖）。
