## Why

driver `agent-model-set-driver` 的第四个子 change。TUI 是日常切换入口，但现在 `s` 只能选一个模型、`m` 也只换单个模型；Agent 模型集落地后，若 TUI 仍按单模型收集输入，用户就没法在 TUI 里表达「默认全选」或收窄子集。

## What Changes

- `s` 流程改为：选 agent → 多选 Agent 模型集（space 勾选，进入时默认全选）→ 选默认模型 → 确认
- `m` 保持「仅换默认模型」：在已写入的 Agent 模型集内选择，不改模型集
- 结果提示包含 Agent 模型集条数与默认模型；codex 仍提示需暴露的环境变量名
- AI Tab 的 agent 行展示与 status 口径一致（默认模型 + 条数 + 漂移）
- README「TUI mode」键位表与 `.agents/skills/senv-cli/SKILL.md` 的 TUI AI Tab 说明同步

## Non-goals

- 不改 CLI flag 表面与 status 命令实现（cli 子 change）
- 不改写回、投影与清理实现（agents 子 change）
- 不新增 senv 自己的模型选择语义，TUI 仍只调用 SwitchManager

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider-tui`: Tab 内切换操作改为多选 Agent 模型集并选定默认模型，「仅换默认模型」限定在已写入集合内

## Impact

- `internal/tui/ai_tab.go`：切换状态机、多选交互、结果与漂移展示
- 测试：多选与默认模型选择、空选拦截、`m` 语义、失败回显
- 文档：README「TUI mode」、`.agents/skills/senv-cli/SKILL.md`

## 验证记录

- 2026-09-11：`go test ./internal/tui/...` 通过，新增/更新 `internal/tui/ai_tab_test.go`：`s` 多选默认全选、取消勾选后只写子集（`settings.json` 不含未选模型）、空集在提交前拦截且不落指针、档案默认模型不在勾选集合内时游标落首项、`m` 候选只用指针里的 Agent 模型集且提交时模型集不变、agent 行展示 `provider / 默认模型（N 个模型）` 与 `⚠` 漂移标记且不渲染凭据、三步 Help 文案。
- 2026-09-11：`make check` 全绿；`openspec validate agent-model-set-tui --strict` 通过。
- 文档：README「TUI mode」键位表与 `.agents/skills/senv-cli/SKILL.md` 的 AI Tab 说明已与 `Help()` 同步。
