## Why
ADR-0020 定稿将 `ssh_host`/`ssh_keypair` 接入同步通道。ssh-assets 规格的「加密与同步不变性」早已声明 host/keypair 复用 git/server 同步通道，但 server 模式白名单（syncschema 七 kind）实际不含这两个 kind，spec 与代码矛盾，本次对齐。

## What Changes
- `internal/syncschema`：新增 `KindSSHHost`/`KindSSHKeypair` 与 `ValidateIdentity` 分支（grp 空、key=别名安全单路径段）
- `internal/provider/server_state.go`：`collectEntriesDiff` 增 `hosts/`、`keypairs/` 遍历段；`entryLocation` 增两个落地 case
- `cmd/sync.go`：`isConfigSourceKind` 扩入两个新 kind（冲突对照提示覆盖 SSH）
- `internal/conflict/render.go`：两新 kind 不加明文解码分支，保持元数据渲染
- `internal/server/store`：无代码改动（白名单来自共享包），补校验对齐测试
- specs delta：server-sync 两个 MODIFIED requirement、ssh-assets「加密与同步不变性」MODIFIED

**安全性分析**：档案 blob 含私钥本体，走既有 AES-256-GCM 加密 blob 通道，server 零知识不变；冲突报告仅元数据渲染，测试断言私钥明文不出现在任何渲染输出。

## Non-goals
- host export 缺失引用 warning（`ssh-sync-export-warning` 切片）
- senv-server 代码改动与镜像发布（发布顺序 server 先行，属运维动作）
- git 模式行为（vault 整目录 git 分发，本就不经白名单）
- TUI、per-kind 同步开关、keypair 拆分（ADR-0020 rejected）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支（由 driver 准备段完成） |

## 验收标准
- [ ] 九 kind 白名单：两新 kind 合法身份通过，grp 非空/key 为空/路径语义身份被拒，既有七 kind 用例不变
- [ ] 本机 `hosts/`、`keypairs/` 的 `.enc` 档案进入待推送集合；pull 条目落回原目录（0600），增量快照行为一致
- [ ] 冲突报告对两新 kind 出 alias+revision 对照提示，渲染输出断言不含档案明文与私钥字符串
- [ ] server store 对两新 kind 的接受/拒绝与 client 白名单一致（测试覆盖）
- [ ] `go test ./internal/syncschema/ ./internal/provider/ ./internal/server/store/ ./cmd/... -race` 全绿
