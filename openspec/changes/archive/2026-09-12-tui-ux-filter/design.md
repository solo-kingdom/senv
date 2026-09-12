# Design: tui-ux-filter

## Context

现状 4 份过滤状态机复制（env/text/config/audit），SSH/AI/MCP 缺失。② 已落地 keymap 注册表，③ 已提供共享列表组件；过滤词是列表可见集的输入之一，自然挂在组件旁。

## Goals / Non-Goals

**Goals:** 一份过滤状态机；全 Tab 一致行为；SSH/AI/MCP 补齐。

**Non-Goals:** fuzzy、value 匹配、全局 `S` 改动、多选联动（⑤ 处理「`a` 全选=过滤可见集」）。

## Decisions

- **组件形态**：`Filter{ active bool; term string }` + 三个事件（append/backspace/clear），渲染为列表窗标题旁的过滤提示行；`Visible(n)` 谓词由 Tab 提供匹配函数（统一调 `matchKey`），组件不认识业务字段。备选「组件内建字段名匹配」被否：各行标识字段不同（key/alias/hostname/command）。
- **双栏作用范围**：SSH/AI/MCP 过滤只作用于左栏主列表；右栏是「当前选中项的联动视图」，随左栏光标联动刷新。config 的「过滤 + 侧栏计数」由 config-tui spec 另有约束，保持其现行为。audit `f` 预设循环 = 在自由文本之上叠加的枚举谓词，作为扩展点保留。
- **状态统一**：`esc` 清过滤并退出输入（回上一层规则的一部分）；过滤词变更时游标重置到 0（与现有 4 处行为一致，迁移不引入新差异）。

## 数据流与错误处理

过滤是纯内存谓词，不改数据装载；匹配函数错误不可能（子串匹配无 error）。SSH/AI/MCP 懒加载列表在数据未就绪时过滤自然为空集，不新增加载路径。

## Risks / Trade-offs

- [AI/MCP 右栏联动在过滤后选中项变化，用户感知跳动] → 右栏内容本就绑定左栏光标，行为与不过滤时一致；联动词义写入 spec scenario 验收
- [4 处迁移引入行为差] → 迁移仅替换状态机实现，匹配函数、游标重置、esc 语义逐点核对

## Open Questions

无。
