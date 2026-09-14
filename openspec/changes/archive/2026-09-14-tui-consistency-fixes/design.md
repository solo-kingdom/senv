## Context

`KeyAction` 注册表（keymap.go）是键位唯一真相源：Tab 的 `Update` 用它分发，`?` 总览（help.go 的 `newHelpTab` 快照 `Bindings()`）与底栏（model.go:634 `groupBar(Bindings())`)用它渲染。审计发现分发侧实现了若干按键却未注册：KeyPair 删除确认态 `F`（keypair_tab.go:465，确认页文案 :940-941 自证存在）、Env/Text/Config/SSH/MCP 的多选 `space`/`a`（env_tab.go:389/396、text_tab.go:348/354、config_tab.go:482/488、ssh_tab.go:439/445、mcp_tab.go:422/428）。AI/Audit/History Tab 经比对一致。详见 proposal.md。

## Goals / Non-Goals

**Goals:**
- 所有 Tab 分发处理的按键在 `?` 总览与底栏可见，帮助双向不漂移
- 用例把「分发 ⊆ 注册」固化为可执行契约，回归即红
- README 快捷键表与实现逐键一致（以代码为准）

**Non-Goals:**
- 不改任何按键行为与分发逻辑；不做源码解析式差集扫描（proposal 已列）
- 不为各 Tab 的静态 `Bindings()` 补全 modal 态分支（env/text/config 的确认键仍按既有静态列表呈现，属既有设计取向）

## Decisions

### D1 共享 `act*` 常量承载多选两键

在 keymap.go 按既有 `actNew`/`actExport` 范式新增 `actSelect`（`space`，toggle select）与 `actSelectAll`（`a`，select all visible），分组挂 `grpItem`。五个 Tab 的 `Bindings()` 直接引用常量，与分发侧手写字符串同源。

- 备选：各 Tab 字面量各自注册——违背 keymap.go:62-63「双侧共用同一常量」的既有约定，同义键描述会漂移。
- 备选：改 Update 分发改用 `Matches`——动作太大且触碰行为，违背 Non-goals。

### D2 KeyPair `F` 注册进删除确认态分支

`Bindings()` 的 `kpModeDeleteKey` 分支（keypair_tab.go:96-99）在既有 confirm/cancel 两行后追加 `{F, force delete (clear host identityKey), grpConfirm}`，与确认页文案同语义。

- 备选：把 `F` 提升为全局共享动作——它是 KeyPair 独有动词，无共享价值；且 spec 明确「同一动作同键」而非「同键全 Tab」。
- 注：`kpModeMaterialize` 的 force 由 `pendingForce` 字段驱动、无独立按键，不注册。

### D3 契约表驱动的一致性测试

新增 `keymap_consistency_test.go`：对每个 Tab 构造实例（含 KeyPair 删除确认态），表驱动断言「该 Tab Update 实际分发的键集合 ⊆ `Bindings()` 注册的键集合」。表内逐键显式列出期望值（如 ssh host 栏含 `space`/`a`），不做源码正则扫描。

- 备选：解析 Go AST/正则扫 `case "`——脆弱、误报多（同义键、条件分支），违背项目「避免过度设计」。
- 测试放 keymap 语义近旁：它就是 keymap 注册表契约的回归网。

### D4 README 勘误以代码为准

README 4 处按实现改写并核对 SKILL.md（其已准确）。刷新行同时点明「重命名仍是 `r`」避免与 env 条目栏 `r` 混淆——README:347 原行只写「刷新」，改为 `ctrl+r` 并括注「条目栏 `r`=重命名」。

## 数据流图

```
按键 (tea.KeyMsg)
   │
   ├─→ Tab.Update ──switch msg.String()──→ 执行业务动作        【分发侧，已存在】
   │
   └─→ Tab.Bindings() ──→ []KeyAction (keymap 注册表)
                              ├─→ model.go groupBar()        → 底栏分组提示
                              └─→ help.go newHelpTab() 快照   → `?` 总览 overlay
本 change 只补「注册侧」的缺员（actSelect/actSelectAll/F），
让两条消费路径看到与分发侧一致的键集合；不改按键 → 动作的数据流。
```

## 错误处理策略

- 纯注册/文档变更，无运行时错误路径新增；`Bindings()` 无副作用、无失败态。
- 一致性测试失败即编译期/测试期暴露（键缺失是静态事实），无需运行时降级。
- README 勘误若与代码再漂移：契约测试只管 keymap，文档漂移靠 code review 与本 change 的验收标准兜底。
- 回滚：revert 单个 commit 即恢复注册表缺员状态，无数据迁移与兼容性问题。

## Risks / Trade-offs

- `?` 总览行数增加 2 行/Tab，极端矮终端下帮助浮窗滚动增加 → 可接受：gridRows 已支持滚动，且完整可见性优先于少滚动一行。
- 契约表需随新按键手动维护，漏更新会在加新键时红测试 → 正是设计意图（把漂移变成测试失败）。
- `space` 在 AI 向导内已有勾选语义（ai_tab.go:491，已注册）；列表多选 `space` 与其是不同上下文，不构成冲突。

## Migration Plan

无迁移。随常规发版走；README/SKILL.md 与代码同 PR 更新。

## Open Questions

无。
