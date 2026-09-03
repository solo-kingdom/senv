## Why

server provider 目前完全依赖手动 `senv sync`：读无新鲜度保障（可能导出旧值），写积压不可见（冲突概率随时间上升），与"中心化 server-first 多机同步"的产品预期不符。同步应内嵌进命令生命周期，server 可达时无感完成。

## What Changes

- 读命令（env 导出 / list / text 读 / session / TUI 打开）执行前按节流窗口 best-effort 自动 pull：超时预算 1~2s，失败静默降级本地缓存，不增加可感知延迟
- 写命令本地写入立即返回，进程退出前 best-effort push；push 失败（网络/冲突）不使命令失败，留 dirty 待后续命令自动重试，仅输出一行警告
- 关键低频写（`passwd`、init 后首次写入）改为阻塞式、必须成功的 push
- sync state 新增节流时间戳；同步段用 flock 串行化，防止同机多进程并发竞争
- settings 新增 `auto_sync`（默认开启）与节流窗口配置；`--refresh` 绕过节流强制拉取
- `senv sync` 保留为状态查看与冲突修复（`--accept-remote` / `--force-push`）入口

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `server-sync`: 新增自动同步需求——读路径节流 pull、写路径退出前 best-effort push 且命令不因 push 失败而失败、关键写阻塞 push、flock 串行化、`auto_sync` 配置与 `--refresh` 逃生口

## Impact

- `internal/provider/`：server.go 新增 AutoPull/AutoPush（超时预算、降级语义），server_state.go 增加节流时间戳，新增 flock
- `cmd/`：读写命令接线与警告文案
- settings 结构新增字段（向后兼容）
- 不改加密格式、本地缓存格式与 server 端 API

## 非目标

- 不引入 daemon / watch 常驻进程
- 不做自动冲突合并（v1 仍只检测，人工解决）
- 不改变 git provider 的手动 commit/push 行为
- 不改 server 端 API

## 安全性分析

- 自动同步只增加密文与 Bearer token 的传输时机，不新增明文链路；仍强制 https（`ValidateServerAddress`）
- vault 口令不出本机；自动 pull 落盘的仍是密文缓存
- 节流时间戳与 flock 锁文件落在缓存目录（0700），不引入新的敏感面
