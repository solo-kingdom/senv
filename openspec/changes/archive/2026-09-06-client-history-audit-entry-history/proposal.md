## Why
server 模式下条目仅存最新密文，误改/误删后无法回看或找回；git 模式有 git 历史而 server 模式没有。需为每个条目保留最近密文历史（默认 3 版），支持 cli+tui 查看（含日期）与单条目恢复。

## What Changes
- server 新增 `entries_history` 表（0003 迁移）：条目被修改/删除前留存旧密文版本，按条目保留最近 N 版（默认 3，server 启动参数 `--history-retain` 可配）
- 新增历史查询端点：按条目返回历史版本（revision、服务端时间戳、密文；含已删除条目的最后版本）与 vault 级最近变更
- client 新增 `senv history` 命令与 TUI 历史视图：本地解密展示、日期时间显示、与当前值对比；支持单条目恢复（复用既有推送流程，含误删找回）

## Non-goals
- 整库快照与整库回滚；vault metadata 历史；git 模式历史（git 自身覆盖）

## Capabilities

### New Capabilities
- `server-history`: server 模式条目级密文历史的留存、保留策略、查询与单条目恢复

### Modified Capabilities
（无——push/pull 既有语义不变，历史留存是推送的附加副作用）

## Impact
- server：internal/server/store（历史写入、保留裁剪、查询）、internal/server/handler（历史端点）、internal/server/migrate（0003）、senv-server/main.go（--history-retain）
- client：新增 cmd/history.go 与 internal/tui 历史视图；恢复走既有 collectDirty/Push 路径
- 隐私：历史仅存密文，client 用现有口令派生密钥解密展示

## 安全性分析
- 历史行仅含密文与 revision/时间戳，server 无法解读（零知识不变）
- 恢复走既有乐观锁推送，不绕过冲突检测
- 保留裁剪只删历史旧行，不触碰 entries 本体

## 验证记录
- 2026-09-06：`make check` 全绿，含新增 store/handler 历史测试与 provider e2e。
- 2026-09-06：闭环验证以自动化 e2e 承担：`TestE2EEntryHistoryAndRestore` 覆盖 修改产生历史（含 server 时间戳）→ 按条目/vault 级查询 → 恢复旧值产生新 revision → 删除后从历史找回 → 恢复后再 sync 无冲突；`TestHistoryUnsupportedOnFake` 验证旧 server 降级报错。
- 2026-09-06：保留语义澄清（spec 与实现一致）：条目首次创建无前像（没有旧值可留），仅修改/删除产生历史；每条目保留最近 N=3 版。
- CLI：`senv history`（vault 最近）/ `senv history kind:group:key`（条目时间线）/ `--restore REV`（交互确认，--yes 跳过）；TUI 第 4 个 Tab「History」（server 模式自动注册，git 模式隐藏）。
