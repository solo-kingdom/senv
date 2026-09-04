## Why

macOS 上 `session start` 完全不可用：`/var` 符号链接触发 fail-closed，且 darwin 无 memory-backed 探针、系统本身没有 tmpfs。当前"仅 tmpfs"策略在 macOS 无任何可行实现，迫使用户放弃 session 或放弃 macOS，两者都不可接受。

## What Changes

- 引入 `SessionStore` 抽象，收敛现有 tmpfs 缓存实现，Linux 行为不变
- macOS 默认使用 Keychain（generic-password item）存储 session cache
- 修订安全策略：从"仅 OS 确认的 memory-backed 文件系统"改为"平台验证安全存储"（Linux tmpfs/ramfs；darwin Keychain）
- 修复 runtime root 对系统自带 symlink 的误伤：解析后再校验，写入仍走 no-follow 锚定
- 新增显式 opt-in 磁盘逃生舱（headless macOS/CI），带醒目警告
- 缓存不可用时输出可行动的错误指引
- timeout、boot ID、单 session 语义均不变

## Capabilities

### New Capabilities

- 无

### Modified Capabilities

- `session-auth`: session 缓存存储要求从"仅 memory-backed tmpfs"改为"平台验证安全存储"，并允许显式 opt-in 的磁盘逃生舱

## Impact

- 代码：`internal/session`（cache、runtimefs、新 store 抽象与 darwin 实现）、`cmd`（错误提示与逃生舱开关）
- 文档：README、SECURITY、RELEASE_NOTES
- 依赖：darwin 平台新增 Keychain 访问（经 `/usr/bin/security`，无新增 Go 依赖）

## Non-goals

- 不实现 agent/daemon 模型（后续独立 change）
- 不改变 session 生命周期语义（restart/never、boot ID、uid 级单 session）
- 不做进程级隔离（见安全性分析）

## Security Analysis

- **Keychain（darwin 默认）**：静态加密、随 keychain 锁定；写入时将 `/usr/bin/security` 加入 item trusted apps，保证 MCP 每请求读取零弹窗。诚实定位：同用户下能 exec `security` 的进程可静默读取——这是用户态加密存储，显著优于裸磁盘文件（加密、锁定联动、不进同步/备份、无散落文件），但不提供进程级隔离；Linux tmpfs 路径安全等级不变
- **磁盘逃生舱**：默认拒绝；仅显式 opt-in，保持 0600/0700、原子写、boot ID 校验，并输出醒目警告
