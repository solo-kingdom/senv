## Context

`internal/tui/ai_tab.go` 现在的切换状态机是 `aiFlowSelectModel → aiFlowConfirm`，用 `modelIndex` 选单个模型；`m`（`flowOnlyModel`）同样只选一个模型。切换子 change 后 SwitchManager 接收模型集与默认模型，agent 行展示口径也随 cli 子 change 变化。

## Goals / Non-Goals

**Goals:** 用最小交互改动表达「多选模型集 + 默认模型」；空集可拦截；`m` 限定在已写入集合；展示与 status 口径一致

**Non-Goals:** CLI 参数、写回实现、清理算法、指针结构；不引入 senv 自己的模型选择语义

## Decisions

1. **两步选择而非新表单**：`s` 的状态机扩展为「多选模型集 → 选默认模型 → 确认」，复用现有 `aiFlowSelectModel`/`aiFlowConfirm`，只把单选改为 `selected map[int]bool` + 一个默认模型游标。备选：弹表单（重，且与现有键位风格不一致）。
2. **进入多选步骤时默认全选**（grill D3），与 CLI 省略 `--models` 的语义一致；默认模型游标停在档案默认模型（不在集合内时停在集合首项）。
3. **`m` 不再进入多选**：直接从「当前 Agent 模型集」构造候选并进入默认模型选择，避免在旧集合外选到未写入的模型（grill D9）。
4. **空集拦截在提交前**：确认时若选中集合为空，提示并停留在当前步骤；不调用 SwitchManager。
5. **漂移展示复用指针 vs 档案比对**（同 cli，grill D4）；Tab 不解析 agent 配置文件。

## 数据流

```
按键 s → 选 agent → 多选模型集（默认全选）→ 选默认模型 → 确认
                                                  │
                                        SwitchManager.Switch(agent, provider, models[], default)
                                                  │
                                       成功 → 刷新指针/行展示 + 提示（条数、默认模型、codex env 名）
                                       失败 → 统一提示条，行与配置不变

按键 m → 已指向？否则提示先切换 → 候选 = 指针中的 Agent 模型集 → 选默认模型 → 同上（模型集不变）
```

## 错误处理策略

- 空模型集：提交前拦截，界面提示，不进入 SwitchManager
- SwitchManager 错误：统一提示条回显原因，指针与配置不变（复用既有回滚）
- 模型集内不含档案默认模型：默认游标停在集合首项，不报错

## Risks / Trade-offs

- [多选交互增加键位复杂度] → 保持「space 勾选 / ←→ 移动 / enter 确认 / esc 取消」，并在 Help() 中给出说明
- [大模型集下列表过长] → 沿用既有列表截断与滚动策略，不新增分页

## Migration Plan

无数据迁移；TUI 是即时交互，旧键位语义变化在 Help() 与 skill 文档中说明。

## Open Questions

无
