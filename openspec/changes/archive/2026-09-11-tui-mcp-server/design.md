## Context

CLI 已提供 MCP Server 档案 CRUD 与导出/撤回（`internal/mcp.Manager` / `Exporter`、本机台账、ADR-0007/0008）。TUI 的 `Managers` 尚未注入 MCP，Tab 条也没有对应项。见 proposal.md 的 Why。约束：不改 vault schema；不把 `mcp install` 放进 TUI；导出目标集合必须与 CLI 相同（`agentcfg.Supported()`，不是 AI Tab 的 switch 表）。

## Goals / Non-Goals

**Goals:**

- 把既有 manager/exporter 接到一个独立 MCP Tab，交互对齐 AI Tab（两栏）与 Config Tab（计划确认）
- 导出粒度固定为「当前档案 × 当前/全部 agent」，Force 在计划页按次选择，而不是进程级开关

**Non-Goals:**

- 不扩展 Exporter 的传输类型、scope 或 `--print`
- 不把 `mcp_servers` 纳入 server 同步清单
- 不新增表单字段类型（args/env 复用既有 `formEditor`）

## Decisions

1. **独立 Tab，不并入 AI。** AI Tab 已承担 provider CRUD + 切换向导；MCP 的计划页、明文标注与另一张 agent 表会挤爆同一 help/键位空间。备选（并入 AI）否决。

2. **装配方式对齐 SSH，而不是 LLM。** vault 解锁后始终注入 `mcp.Manager`；nil 仅保留给测试。Exporter 在 Tab 内按次构建：默认 `Force=false`、`Scope=user`，Resolve 闭包走已有 env/text 解引用。用户在计划页按 `F` 时用 `Force=true` 重新 `Plan`，不改 Exporter 的构造期语义。

3. **x/X 的档案维始终是左栏当前项。** `Plan(targets, []string{alias})` 显式传 alias，避免空 slice 被 Exporter 解释成「全部档案」（那是 CLI 省略 alias 的语义，TUI 明确不做）。`X`/`U` 的 targets 为 `agentcfg.Supported()`。

4. **env 主视图不持有可渲染明文。** 表单 env 字段在 TUI 内只展示键名；打开 `$EDITOR` 时才把 `KEY=VALUE` 写入 0600 临时文件。提交时再 parse 回 map。这与 AI 凭据 `formSecret` 不同，但与 Text/Config 的 `e` 同构（ADR-0005）。

5. **右栏状态用 Plan 结果，不另造判定。** 对当前档案、全部 targets 跑一次 `Plan`（不确认、不 Execute），把每条 action 映射为 未导出 / 已导出(skip 或可 update) / 漂移。台账损坏时沿用 Exporter 既有「全部当外部条目」行为。

6. **删除只走 `Manager.Delete`。** 确认框列出台账中的 agent id，文案写明用 `u` 撤回。不调用 Unexport。

## 数据流

```
senv tui
  └─ Managers.MCP ──▶ mcp.Manager (vault mcp_servers/)
         │
         ├─ n/e/d ──▶ Add / Update / Delete
         │
         └─ x/X/u/U
              │
              ▼
         NewExporter(Force, Resolve, LedgerPath)
              │
              ├─ Plan(targets, [currentAlias]) ──▶ 计划页
              │         │
              │         ├─ F ──▶ 以 Force=true 重算 Plan
              │         ├─ esc ──▶ 丢弃，不写盘
              │         └─ y ──▶ Execute / ExecuteUnexport
              │                    （撤回 changed 走逐条 y/n）
              └─ 右栏状态：同一 Plan 的只读投影
```

## 错误处理策略

| 场景 | 行为 |
|------|------|
| 无选中档案按 x/u | toast，不打开计划 |
| command 为空 / alias 冲突 | 表单内联错误，不写 |
| 引用解析失败 | 计划条目标 error；该 agent 不写盘，其余继续 |
| 单个 agent 文件不可写 | 与 CLI 相同：其余继续，toast 汇总失败数 |
| 台账损坏 | Exporter 警告；TUI toast 台账警告，状态按外部条目处理 |
| `$EDITOR` 失败 | 表单保持打开，错误条显示原因，临时文件清理 |

## Risks / Trade-offs

- [Force=true 重算 Plan 可能与第一份计划条目数不同] → 计划页必须展示重算后的版本，确认只针对当前屏幕上的计划
- [`$EDITOR` 临时文件含字面量] → 0600 + 退出后删除，与 Text/Config 相同；不把值写入 toast/审计
- [右栏每次切档案都跑 Plan，会读 agent 配置文件] → 与 CLI dry-run 同类 IO；档案与 agent 数量都很小。若日后变慢再缓存
- [用户以为删档案会撤回] → 确认文案强制列出已导出 agent

## Migration Plan

无数据迁移，无 CLI 破坏性变更。旧 vault 无档案时 MCP Tab 显示空态。回滚即不再注册该 Tab。

TUI 键位（实现后写入 senv-cli skill）：

```
MCP Tab: ↑↓ 移动 · ←→ 切栏 · enter 详情 · n 新建 · e 编辑 · d 删除
         x/X 导出当前/全部 agent · u/U 撤回 · 计划页 y 确认 / F 覆盖漂移
```

## Open Questions

无
