## Why

TUI 首屏加密快照 `tui-snapshot.enc` 落在 `<dataPath>/`，被 server 同步的「顶层任意 `.enc` 即 config 条目」扫描收成 `config//tui-snapshot`。快照每次 TUI 退出/写操作都以新 nonce 重写，hash 必变，于是每次会话都产生待推送；本机历史推送或其他机器/共享 vault 各自推自己的本地快照，就会撞乐观锁产生 409 假冲突——本次实测即为 `local rev 1182 / remote rev 1183`。同一批机器本地文件（同步 state、锁、`agent-pointers.json`、模型目录缓存）在各扫描点处理不一致：rekey 把快照当未索引 config 直接报错、orphan 检测把它当用户密文、git 模式会把它和 `agent-pointers.json` 提交推送。

## What Changes

- 在 storage 层引入单一「机器本地工件」登记表，区分 `dataPath`/`configPath` scope，作为唯一事实来源。
- 所有扫描点统一查询该表：server 同步 collect、TUI 指纹、rekey 分类、orphan/一致性检测、git add 排除路径与生成的 `.gitignore`。
- 补齐此前遗漏的机器本地文件：`tui-snapshot.enc`、`.senv-sync-state.json`、`.senv-sync.lock`、`.senv-vault.lock`、`agent-pointers.json`、`cache/`。
- 已存在于远端的 `config//tui-snapshot` 由首次同步的删除标记清理。

## Capabilities

### New Capabilities
- （无）

### Modified Capabilities
- `server-sync`: 同步收集排除机器本地工件，并清理远端历史误收条目。
- `tui-startup`: 明确快照为机器本地缓存，不得进入同步集合/版本库。
- `rekey-recovery`: rekey 预检跳过机器本地工件，不再误判为未索引 config。
- `data-consistency`: init 防呆与一致性探针忽略机器本地工件。
- `git-sync`: git add 与 `.gitignore` 覆盖全部机器本地工件。

## Non-goals

- 不把 config 采集改为 config index 驱动：当前 server 端 index 为空却有 36 个 config 密文（历史遗留），index 驱动会造成删除误判和数据丢失。
- 不迁移快照文件位置，不改其加密格式/权限，不改 server 协议或 schema。

## 安全性分析

登记表内文件要么是已加密载荷（快照、state），要么是无密文元数据/锁（sync lock、vault lock、agent 指针）。本变更不新增密钥材料、不改变加密姿态。修复消除的是「本机状态被当作 vault 条目推到共享 server/git 远端」的泄漏面与假冲突，并让 `agent-pointers.json`（本机 provider/model 指向，ADR-0003 声明机器本地）不再被 git 模式提交。

## Impact

- 影响代码：`internal/storage`（新增登记表并接入 rekey/orphan/gitignore）、`internal/provider/server_state.go`、`internal/tui/snapshot_cache.go`、`internal/git/manager.go`。
- 影响数据：首次同步向 server 推送 `tui-snapshot` 的删除标记；git 仓新增若干忽略规则。
- 无 CLI/API 变更，无存储格式变更。
