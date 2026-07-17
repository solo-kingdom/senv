## Why

无 session 时一次 `senv env export` 会经 `getEnvManager` → `resolveValue` → `getEnvManager`/`getTextManager` 多次调用 `resolveAuth`，正确密码也要输入多次；且 `eval $(senv env export)` 成功输出被吃掉、无引导。需在同一次进程内复用认证，并在无 cache 时清楚提示开启 session。

## What Changes

- 同一次 CLI 进程内：首次成功鉴权后复用（内存级），后续 `get*Manager` / `resolveValue` MUST NOT 再提示密码
- 举一反三：所有经 `resolveAuth` 的入口共用同一进程内认证结果
- 无有效 session 且非交互（如被 `eval $(...)` / 非 TTY）时：MUST NOT 弹密码；stderr 清楚提示执行 `senv session start`
- 交互式功能命令仍可临时要密码一次（不落盘）；成功后本进程内不再问
- 文档：zshrc 示例改为依赖 session，避免裸 `eval` 在无 cache 时卡密码
- **非目标**：不恢复「功能命令隐式 `StartSession` 写盘」；不改加密算法；不改 `session start` 为唯一写 cache 入口

## Capabilities

### New Capabilities

- （无）

### Modified Capabilities

- `session-auth`: 增加「单次进程内鉴权复用」；明确无 session 时非交互路径禁止 prompt、须提示 `session start`；保留「仅 session start 写 cache」

## Impact

- 代码：`cmd/auth.go`、`cmd/env.go`、`cmd/text.go` 及所有 `get*Manager`/`resolveAuth` 调用方；相关测试与 SESSION_USAGE/README 示例
- 安全：进程内缓存 derived key/密码仅存活于单次 CLI 生命周期，退出即失；仍不因功能命令落盘 session。非交互拒 prompt 降低 shell 启动时误交互面
- 兼容：有 session 行为不变；无 session 的 `eval $(senv env export)` 从「弹 N 次密码」变为「失败并提示 start」（**BREAKING** 对依赖启动时临时密码注入的用户）
