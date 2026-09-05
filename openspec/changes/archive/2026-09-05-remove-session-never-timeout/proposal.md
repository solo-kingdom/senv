## Why

`never` 与 `restart` 在实现中功能完全等价（相同校验分支，同受 boot-ID 约束），仅剩意图标签差异，且「never expires」提示易被误解为可跨 logout/reboot 存活，与派生密钥只驻留易失性/平台安全存储的不变量相悖。移除 `never` 消除语义混淆，将超时模型简化为 duration/restart 两类。

## What Changes

- **BREAKING**: 移除 `never` 超时类型。`ParseTimeout` 不再接受 `never`/`infinite`/`forever`，CLI 帮助、成功提示、status 输出与文档全部移除该选项
- 历史遗留 cache（`timeout_type: "never"`）按未知类型判为过期，用户重新执行 `senv session start -t restart` 即可
- settings.json 配置 `session.timeout: "never"` 时 `session start` 报错并列出可用值
- 同步更新 `SESSION_USAGE.md`、`README.md`、`docs/SECURITY.md` 与相关测试

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `session-auth`: 超时模式枚举从 duration/restart/never 收窄为 duration/restart；新增超时值校验要求（拒绝 `never` 并提示可用值）；改写复用、安全存储、MCP 授权 requirement 中引用 `never` 的场景

## Impact

- 代码：`internal/session/`（types.go、timeout.go、manager.go）、`cmd/session.go`、`cmd/init.go`、`cmd/env.go` 文案
- 测试：`internal/session/*_test.go`、`cmd/*_test.go` 中 `never` fixture 改为 `restart`
- 兼容性：旧 `never` cache 一次性失效，重建成本为一次密码输入

## 非目标

- 不改变 `restart` 与 duration 的任何语义
- 不迁移旧 `never` cache 为 `restart`（直接过期，不自动转换）
- 不修改 `--insecure-cache`、MCP 授权机制、审计日志结构或默认超时（8h）

## 安全性分析

不触及加密算法、密钥派生与存储介质校验，属攻击面收缩：移除易被误读为「永久留存」的选项，所有模式仍强制平台安全存储与 boot-ID 校验，安全属性只增不减。
