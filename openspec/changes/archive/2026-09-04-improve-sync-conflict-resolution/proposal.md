# Improve sync conflict resolution

## Why

当前 server 同步冲突只输出条目标识和远端 revision，用户无法判断本地与远端差异，只能在 `--accept-remote` 与 `--force-push` 之间盲选；`config_index` 与 vault metadata 同时冲突时尤其容易误判。

## What Changes

- 冲突报告将包含本地 base revision、远端 revision、删除状态、密文大小、hash 与可用更新时间，并说明 metadata key 兼容性。
- `senv sync` 在 TTY 下进入交互式冲突解决器：可查看安全摘要、按键查看明文对比，并确认后采用本地、远端或手动合并结果；非 TTY 保留现有命令式指引。
- 新增 `--no-interactive` 作为显式退出交互的开关；现有 `--accept-remote` / `--force-push` 行为保持兼容。
- 对 `text`、UTF-8 `config`、`env` 与 `config_index` 提供可选 editor 手动合并：生成 LOCAL/REMOTE 两方缓冲区，校验最终内容后暂存、确认并基于远端 current revision 推送。
- server API 在 pull 与 409 冲突响应中补充轻量 `updated_at` / size / deleted 描述符；409 不携带密文，内容仍按需通过受 token 保护的同步接口获取。

## Non-goals

- 不实现自动 three-way merge；当前同步状态没有共同祖先密文。
- 不允许 editor 直接修改 vault metadata；metadata 冲突仍需整体选择一边。
- 不在自动 push 警告路径中启动交互 UI；自动同步仍只引导用户执行 `senv sync`。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `server-sync`: 冲突从“仅提示命令”升级为安全对比、TTY 交互选择与可选手动合并。
- `server-api`: pull 与冲突响应补充非机密描述符，继续禁止在 409 中返回冲突密文。

## Impact

- 影响 `cmd/sync.go`、`internal/provider`、server handler/store、类型化内容渲染与现有 editor/TUI 基础设施。
- 需要新增或调整 provider 冲突快照、resolution plan、安全临时 editor 会话与相关测试。
- 兼容旧 server：缺失新增描述符时显示可用地标；缺失远端密文时不降级安全性。

## Security Analysis

冲突摘要默认只暴露非机密元数据；明文查看和 editor 合并仅在 TTY 且获得 vault key 后发生。editor 使用私有一次性目录和 0600 文件，退出后递归清理，不把明文写入日志。vault metadata 只显示安全摘要并检查 key 兼容性，不提供 raw 编辑。409 响应不携带 ciphertext，server 继续保持内容零知识。
