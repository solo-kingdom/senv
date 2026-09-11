# Design: tui-ux-keymap

## Context

按键处理散布在各 Tab 的 `updateKey`/`handleModalKey`/`updateMode` 与 `model.go` 全局 switch；`help.go` 的 `parseHelp` 把 `Help()` 字符串当 keymap 真相源。grill D7 附录给出完整语义表，本 change 是其落地载体。

## Goals / Non-Goals

**Goals:** 注册表成为唯一 keymap 真相源；D7 裁决逐键落地；确认框行为统一。

**Non-Goals:** 不改多选/过滤行为（④⑤）、不做软过渡 alias、不动 Tab 数据逻辑。

## Decisions

- **注册表形态**：`type Binding struct { Keys []string; Action string; Desc string }`；Tab 以 `Bindings() []Binding`（全局组 + Tab 组）声明，`model.go` 按 Action 路由，`help.go` 直接渲染 Binding 列表。`InputMode()` 语义不变（表单/输入态全局键不劫持）。备选「map[rune]func」被否：丢失描述文案与遍历顺序。
- **迁移策略 = 只动按键路径**：本轮不把各 Tab 的渲染/状态迁到共享列表组件（③④⑤ 做），避免一次改动横跨两个子 change 的验收面；冲突仅存在于按键分发处，用注册表接管分发即可收敛。
- **确认框收紧方向**：plan 页「任意其他键=取消」改为「仅 `esc`/`n` 取消，其余键忽略」——误按无效果比误按取消更安全（取消会丢掉整个已生成的计划）。MCP 逐条确认阶段的既有约束（除 `y` 外不放行）不变。
- **`t` toggle 与既有 `a`/`x`**：spec 层面 env 激活/停用两场景保留标题、触发键改 `t`（`validate --strict` 按 scenario 标题严格匹配，不可删改标题，见 driver design「Spec delta 策略」）。

## 数据流与错误处理

注册表只改按键→动作的映射与文案来源，不触数据路径；Action 路由到既有处理函数，错误仍走统一提示条。未注册按键维持现状（多数为忽略或输入字符）。

## Risks / Trade-offs

- [8 Tab 按键一次性切换，老用户肌肉记忆失效] → `?` overlay 与状态栏即时反映新键位；`.agents/skills/senv-cli/SKILL.md` 同步
- [Action 路由漏迁导致个别键静默失效] → tasks 逐 Tab 冒烟清单覆盖高频动作（n/e/r/d/enter/esc/t/x/Ctrl+R）

## Open Questions

无（键位逐键裁决见 grill.md D7 附录，已 settled）。
