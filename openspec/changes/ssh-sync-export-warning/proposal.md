## Why
ADR-0020 D4 定案：「host 先到、keypair 未同步到」是多机场景的常态时序，`host export` 现状无条件写 `IdentityFile ~/.ssh/senv/<name>`，悬空引用要到 ssh 连接失败才暴露。按 `mcp export` 宽松写入 + 逐条 warning 的既有模式，把悬空提前到导出时可见；同时按 AGENTS.md「同一变更」约束更新 `.agents/skills/senv-cli/SKILL.md` 的同步范围说明。

## What Changes
- `internal/ssh/host.go`（export 路径）：导出时检查各 host `IdentityKey` 引用的 keypair 是否在本机 vault，缺失则逐条 warning，片段照常生成
- `.agents/skills/senv-cli/SKILL.md`：同步范围含 SSH 资产（`ssh_host`/`ssh_keypair`，档案含私钥）、host export 的 warning 行为
- specs delta：ssh-assets「导出 OpenSSH config 片段」MODIFIED

**安全性分析**：warning 内容仅含 host 别名与缺失 keypair 名（两者本就是 vault 标识），不触碰私钥材料；导出物仍为 config 文本与路径，无新增解密面。

## Non-goals
- `keypair materialize` 行为（维持 fail-closed，现状已报错带名字）
- `proxyJump` 悬空报错（既有 fail-closed 不变，与 IdentityFile 的宽松语义是有意不对称）
- MCP export、TUI 导出预览的行为变更（TUI 导出走同一 `Export` 路径，warning 随之可见即可，不做界面改造）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支（由 driver 准备段完成） |

## 验收标准
- [ ] host 引用本机 vault 缺失的 keypair 时，`senv host export` 逐条输出 warning（指明 host 别名与缺失 keypair 名）且片段照常生成、仍含该 host 的 `IdentityFile` 路径
- [ ] 全部 keypair 在位时输出与现状一致（无新增噪音）
- [ ] `proxyJump` 悬空仍报错（回归不破坏）
- [ ] SKILL.md 已更新；`go test ./internal/ssh/ -race` 全绿、`go run . ssh host export --help` 语法正常
