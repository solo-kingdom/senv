## Why

CLI/TUI 能力审计发现两处一致性漂移：其一，`internal/tui/keymap.go` 声明帮助与实际行为「结构性一致、不可能漂移」，但多个 Tab 在 Update 分发里实现了按键却没注册进 `Bindings()`，导致 `?` 总览与底栏对这些可用按键失明（KeyPair `F` 强制删除、Env/Text/Config/SSH/MCP 的多选 `space`/`a`）；其二，README TUI 快捷键表 4 处与实现不符（`a`→实为 `t`、`o`→实为 `x`、`m`→实为 `M`、`r` 刷新→实为 `ctrl+r`），SKILL.md 是准的。帮助与文档说谎，用户按图索骥会按错键或找不到能力。

## What Changes

- keymap.go 增补共享动作 `space`（勾选/取消）与 `a`（全选过滤可见集），按既有 `act*` 双侧同源范式
- Env/Text/Config/SSH(host 栏)/MCP Tab 的 `Bindings()` 注册上述多选两键；KeyPair 删除确认态注册 `F`（force delete）
- 新增「帮助与分发一致性」回归用例：逐 Tab 断言分发处理的按键全部出现在 `Bindings()`，防再次漂移
- README TUI 快捷键表 4 处勘误（以代码实现为准）：激活 env 组 `a`→`t`（env_tab.go:432）、导出 text `o`→`x`（text_tab.go:391）、AI 换默认模型 `m`→`M`（ai_tab.go:435）、刷新 `r`→`ctrl+r`（keymap.go:71）；SKILL.md 同步复核（其描述本就准确，无需改键位语义）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: 「键位总览」requirement 增补双向完整性——现有条文只约束「列出的键必须与行为一致」，补「分发的键必须全部注册」，并新增两条 scenario（多选键全列出、确认态 `F` 列出）

## Impact

- `internal/tui/keymap.go`、`internal/tui/{env,text,config,ssh,mcp,keypair}_tab.go` 的 `Bindings()` 与新增测试；README.md 快捷键表；无存储/加密/协议变更
- 纯注册与文档变更：不改变任何按键的实际行为，不动 Update 分发逻辑

## Non-goals

- 不改按键语义、不新增/删除任何按键能力，不重构 Update 分发结构
- 不做「扫描源码 case 分支」式的自动差集测试（维护成本高、易误报），用显式契约表驱动断言
- 不审计 CLI 命令文档（`senv --help` 体系）与 TUI 视觉样式

## 安全性分析

本变更不触碰加密、存储格式或写盘路径，无新增解密面。唯一涉及破坏性操作的改动是让 KeyPair `F` 强制删除（清除 host `identityKey` 引用）在帮助中可见——该键与确认页文案（keypair_tab.go:940-941）早已存在且需确认页显式按下才生效，注册进 keymap 只增加可发现性，不降低任何确认门槛。
