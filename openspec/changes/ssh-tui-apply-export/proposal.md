## Why

ADR-0023 把自动推断的「应用导出」落在了 CLI（`senv host export`），但 TUI 的导出仍是旧纯渲染交互：`x` 预览后按 `w` 还要手填 target 路径，批量表单的开头提示还是旧教学位置 `~/.ssh/config.d`。更糟的是 footgun：批量导出若把目录填进 `~/.ssh/senv/groups/`，写进去的 `<别名>.conf` 会被下一次 CLI 全量导出按幽灵片段自动删除。TUI 需要与 CLI 等价的一键应用导出入口（ssh-host-export-apply Q10-B 推迟项）。

## What Changes

- SSH Tab 新增 `A`（apply）键：host 栏 = 重建当前 host 所在组；分组侧栏 = 重建选中组（「All」伪组 = 全量，含幽灵片段清理）——按组选择复用既有侧栏，不新建选择 UI（`a` 已被 host 栏多选占用，apply 用大写 `A`，`G` 有先例）
- 执行前弹确认框，列将写入的组片段数、待落盘密钥数、Include 注册状态与 warning 计数；`enter/y` 确认后调 `Manager.Apply`（与 CLI 同一编排），结果以摘要 toast 呈现
- 批量导出（`x` 多选）的输出目录与单条导出（`w` 写文件）的目标路径校验：拒绝 `~/.ssh/senv/` 内部路径（含 `groups/`），提示改用 `A` 应用导出；placeholder 改为中性用户目录并说明该目录由用户自持

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `ssh-assets`: 新增「TUI 应用导出」requirement（`a` 键、确认框、作用域语义、批量目录校验与 footgun 防护）；TUI 导出一节的既有行为描述同步更新

## Impact

- `internal/tui/ssh_tab.go`（`a` 键、确认 modal、toast、keyactions/help）、`.agents/skills/senv-cli/SKILL.md`（TUI 按键说明）
- 复用 `internal/ssh.Apply`，无新 manager 能力；MCP 仍只读

## Non-goals

- 持久化「导出状态」台账（确认框计数即状态；对齐 MCP 导出状态展示另议）
- TUI 的 `host unexport` / `keypair prune` 入口（CLI 先行，需求再现再补）
- 改动 CLI apply 语义（本 change 纯消费方）

## 安全性分析

TUI apply 与 CLI 走同一 `Manager.Apply`：落盘沿用 0700/0600 与「存在跳过」，`~/.ssh/config` 只增删 senv 拥有行并留 `.senv-bak`——解密面与写盘语义零新增。交互上加确认框（列出落盘私钥数量），防误触；批量目录校验防用户把导出写进 senv 自有树被幽灵清理误删。

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 单仓变更 |

## 验收标准

- [ ] host 栏 `A` 弹确认框（含组片段/密钥/注册/warning 计数），确认后重建该 host 所在组并 toast 摘要
- [ ] 侧栏 `A`：选中组 = 单组重建；All = 全量重建并清理幽灵片段
- [ ] 批量导出目录与单条导出目标文件指向 `~/.ssh/senv/` 内部时被拒绝并提示改用 `A`
- [ ] `go test ./internal/tui/ -race` 全绿；SKILL.md 已更新
