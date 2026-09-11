# config-tui 增量

## ADDED Requirements

### Requirement: 多选批量安装与卸载

config tab 条目列表 SHALL 支持多选集（`space` 勾选、`a` 全选可见集，语义见 tui-viewer「多选集与批量操作」）。`i`/`u` 在多选集非空时 SHALL 以选择集为范围生成一份合并计划预览（可跨分组，逐条列出动作、目标路径与原因），确认后执行；changed 条目的逐条确认语义与既有要求一致。多选集为空时保持既有单条/整组语义。scope 快捷键 `I`/`U`（整组/全部）SHALL 保留，与多选集并存互不替代。

#### Scenario: All 视图跨组勾选批量安装
- **WHEN** 用户在 All 视图勾选分属 3 个分组的 3 条配置按 `i`
- **THEN** 弹出一份合并计划逐条列出 3 条目标，确认后全部执行

#### Scenario: 空集回落单条
- **WHEN** 用户未勾选任何条目按 `u`
- **THEN** 仅对光标所在条目弹出 uninstall 计划，行为与既有要求一致

#### Scenario: scope 键不受影响
- **WHEN** 焦点在侧栏真实分组按 `I` 触发整组 install
- **THEN** 以该分组为范围弹计划（即使条目列表存在勾选集），两者语义独立
