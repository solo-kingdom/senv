## Why

backup 是持久 vault 数据，必须随 git/server 同步，否则多机无法取用冷备份。本切片把 `backup`/`backup_meta` 接入与 text 同构的同步通道。存储/CLI 由 core 提供。

## What Changes

- `internal/syncschema`：`KindBackup`/`KindBackupMeta`，身份与 text 相同
- `internal/provider/server_state.go`：收集/落地 `backups/`
- 冲突呈现对齐 text（可解密对比），合并上限用 `MaxBackupSize`
- server store 共享白名单校验对齐测试
- skill 同步范围段；server-sync spec delta

**安全性分析**：密文通道零知识不变；冲突可解密对比与 text 相同，list/搜索仍不展示 value。新 client + 旧 server 整批 push 会被未知 kind 拒绝，发布须 server 镜像先行。

## Non-goals

- CLI/TUI/MCP（其它切片）
- git 模式代码（整目录分发，不经白名单）
- per-kind 同步开关

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 由 driver 准备段切分支 |

## 验收标准

- [x] 白名单接受 `backup`/`backup_meta` 合法身份，拒绝非法组合；既有 kind 用例不变
- [x] `backups/` 的 `.enc` 进入待推送集合，pull 落回原路径 0600
- [x] 冲突路径可用且合并不超过 `MaxBackupSize`
- [x] server store 接受/拒绝与 client 一致
- [x] skill 写明 backup 随同步分发；新机器凭口令可取用
- [x] `go test ./internal/syncschema/ ./internal/provider/ ./internal/server/store/ ./internal/conflict/ -race` 全绿
