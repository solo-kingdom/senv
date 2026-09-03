## 1. Provider 层：节流、互斥与自动同步方法

- [x] 1.1 `syncState` 增加 `last_pull_at` 时间戳字段，确认旧 state 文件反序列化兼容（缺字段=零值=立即拉取）。验证：`go test ./internal/provider/` 通过且新增兼容用例覆盖旧格式 JSON
- [x] 1.2 锁文件机制：dataPath 下 `.senv-sync.lock` 非阻塞排它锁，锁文件权限 0600、目录 0700，state 文件不含敏感内容审计（高优先级）。验证：单测断言权限位与锁的获取/跳过/自动释放行为
- [x] 1.3 同步段（pull/push/状态读写）全部移入锁内；锁被占用时返回"跳过"信号而非错误。验证：单测模拟并发持锁，断言另一方跳过且状态一致
- [x] 1.4 实现 `AutoPull(ctx, throttle, refresh)`：节流判断 + 超时预算内调用现有 `pull`，成功更新 `last_pull_at`；本地 dirty 条目不被覆盖（复用现有保护）。验证：单测覆盖窗口内跳过（零网络）、窗口外成功、超时降级三场景（注入假 api 与时钟）
- [x] 1.5 实现 `AutoPush(ctx, budget)`：dirty=0 时 no-op，超时预算内调用现有 `push`，网络错误与 `SyncConflictError` 原样上抛。验证：单测覆盖无 dirty 零请求、网络失败、409 冲突三场景
- [x] 1.6 实现 `PushBlocking(ctx)`：10s 预算的阻塞推送，结果必须确认。验证：单测覆盖成功与失败两场景
- [x] 1.7 `make check` 全绿（provider 层回归：现有 server_sync_test/server_e2e_test 不受影响）。验证：`make check`

## 2. 配置与命令层接线

- [x] 2.1 `ProviderConfig` 增加 `AutoSync *bool`（nil=默认开）与 `SyncThrottle`（默认 30s，解析容错），provider 构造链路透传。验证：settings 加载单测覆盖默认值、显式关闭、非法时长回退默认
- [x] 2.2 新建 `cmd/autosync.go`：`autoPull(cmd, refresh)` / `postRunAutoPush()` / 接线辅助函数；非 server provider 或 `auto_sync=false` 时严格 no-op（不构造网络 client）。验证：单测断言 git provider 与关闭开关下零副作用
- [x] 2.3 读命令接线（env get/export/list、text show/list、config get/list、session start、TUI 打开）并增加 `--refresh` flag。验证：集成测试——可达 server 下窗口外读取先落盘远端更改再返回；`--refresh` 绕过节流
- [x] 2.4 root 命令 `PersistentPostRun` 接入 `postRunAutoPush`（dirty 扫描为纯本地，dirty=0 零网络）。验证：集成测试——写命令后断言 server 收到推送；读命令后无网络请求
- [x] 2.5 关键写接线：`passwd` 与 init 后首次写入走 `PushBlocking`，失败输出强警告与 `senv sync` 指引，本地更改保持生效。验证：单测+集成——断网时 passwd 成功返回且含告警文案
- [x] 2.6 统一降级/冲突警告文案（"⚠ N 条待推送…" / 冲突条目列表 + sync 指引），任何同步失败不改变命令退出码。验证：集成测试断言退出码为 0 且 stderr 含预期警告
- [x] 2.7 MCP `serve` 读工具复用 `autoPull`（节流生效，每请求近零成本）。验证：MCP 集成测试——两次连续读取仅一次网络拉取

## 3. 端到端验证与收尾

- [x] 3.1 双机收敛 e2e：两个缓存目录模拟两台机器，A 写 → B 读自动拉取 → B 写 → A 自动推送收敛，`last_synced_revision` 一致。验证：e2e 测试通过
- [x] 3.2 离线降级 e2e：server 不可达时全部读写命令成功、退出码不变、dirty 积压；恢复后任一命令自动排干积压。验证：e2e 测试通过（对应 spec 离线场景）
- [x] 3.3 并发 e2e：同机并行执行多个写命令，断言无 state 损坏、无条目丢失。验证：`go test -race` 并发用例通过
- [x] 3.4 冲突修复路径 e2e：制造远端更新冲突后 `senv sync` 正常检测并列出条目，`--accept-remote` / `--force-push` 行为与既有语义一致。验证：e2e 测试通过
- [x] 3.5 文档更新：`docs/senv-server.md` 增补自动同步行为、`auto_sync`/`sync_throttle`/`--refresh` 配置说明与降级语义。验证：文档审阅 + `make check` 全绿
