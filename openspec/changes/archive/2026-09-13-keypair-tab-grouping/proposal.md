## Why

SSH Tab 三栏（组侧栏 + Host + KeyPair）在终端宽度紧张时 KeyPair 栏被压到下限以下，host 与 keypair 混在一屏信息密度过高。KeyPair 数量增长后同样需要分组组织。把 KeyPair 提升为独立 Tab（与 SSH Tab 平级），两个 Tab 各自获得完整宽度，KeyPair 复用刚建立的分组侧栏范式。

## What Changes

- `KeyPairEntry` 新增单值 `group` 字段（空 = 未分组）；`senv ssh keypair import --group` 与 TUI 导入表单贯通
- 新增 KeyPair Tab（紧随 SSH Tab 注册，同为 `mgr.SSH` 非空时注入）：分组侧栏（All 置顶 → 组名字母序 (n) → 「未分组」置底）→ KeyPair 列表（名称、指纹摘要、行内 `被 N 个 Host 引用`，零引用灰显「未被引用」，名字序不受引用计数影响）→ `/` 按名称过滤
- 原 SSH Tab 的 KeyPair 栏及全部 keypair 动作（导入/重命名/删除/落盘）迁至 KeyPair Tab；SSH Tab 瘦身为「组侧栏 + Host 列表」两栏（与 Env/Text/Config 一致），Host 行保留内联 `key:keyName(fp)` 引用
- 被引用删除保护语义不变（拒绝并列出引用者，`--force`/显式确认后清引用）
- Tab 注册顺序变化：Env, Text, Config, SSH, KeyPair, AI, MCP, History, Audit（server 模式 9 个，数字键 1-9 仍全覆盖）

## Non-goals

- 全局搜索（`S`）纳入 keypair（现状即不搜索 keypair，维持）
- KeyPair 的 tags 字段
- AI/MCP Tab 分组（仍留后续 change）
- SSH Tab 的 Host 分组行为不变

## 安全性分析

与上一 change 同级：`group` 是 per-entry 加密 JSON 内的新字段，密文存储、零知识同步不变；keypair 私钥遮蔽约束原样迁移到新 Tab（TUI 状态与渲染不得含私钥明文）。MCP 只读集成不变。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `ssh-assets`: KeyPair 字段模型新增 `group`（含 CLI/TUI 编辑入口）；TUI 呈现从「SSH Tab 内嵌 KeyPair 栏」改为独立 KeyPair Tab（分组侧栏 + 列表 + 迁移全部 keypair 动作）
- `tui-viewer`: Tab 注入顺序与数字键直达场景更新（新增 KeyPair Tab）

## Impact

- `internal/storage/types.go`（`KeyPairEntry` 加字段）
- `cmd/ssh.go`（`keypair import --group`、`keypair list` 输出）
- `internal/tui/keypair_tab.go`（新文件，参照 `ssh_tab.go` 侧栏与 `env_tab.go` 范式）
- `internal/tui/ssh_tab.go`（瘦身为两栏，删除 keypair 栏/动作/focus 三态降为两态）
- `internal/tui/model.go`（注册新 Tab）
- `README.md`、`.agents/skills/senv-cli/SKILL.md`（TUI 结构描述）
- 既有 TUI 测试（tab 顺序、ssh_tab 三栏断言）需适配
