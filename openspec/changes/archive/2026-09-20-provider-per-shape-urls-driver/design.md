# Design

## Context

taskflow driver change：方案已由探索任务 `provider-per-shape-urls` 冻结（任务内 ADR 已归档，见 proposal 引用），本 change 只编排子 change，不直接改代码。schema 为 spec-driven；driver `skip_specs: true`，无 spec 增量。

## Goals / Non-Goals

**Goals:**

- 把冻结方案拆成可独立 apply、文件范围不重叠的子 change 并编排其顺序
- 交付进度只认子 change 的 tasks checkbox

**Non-Goals:**

- 本 change 不实现任何代码或文档；子 change 的实现细节见各自 design

## Decisions

- **拆分为两个子 change**：
  - `provider-per-shape-urls-core`：数据面 + 引擎 + CLI（storage schema/校验、baseurl 归一化分族、switch 门禁与地址解析/输出来源、`--shape-url` 与 show/list）。数据模型变更集中在此。
  - `provider-per-shape-urls-surfaces`：交互面 + 文档（TUI 表单/详情、MCP 白名单视图、SKILL.md 回写、仓库 ADR 晋升）。
  - 依赖方向：surfaces 消费 core 的 schema 字段与 switch 行为，apply 顺序 core → surfaces；两切片文件范围不重叠。
- **SKILL.md 整体放 surfaces 一次回写**：避免两个子 change 改同一文件；core 的 CLI 行为先落地，surfaces 按最终行为回写文档。
- **spec delta 映射**：core → `llm-provider`（ADDED 形态地址）、`llm-provider-switch`（ADDED 形态地址门禁 + MODIFIED 接入地址按协议族写回）；surfaces → `llm-provider-tui`（ADDED 形态地址表单与详情）、`llm-provider-mcp`（MODIFIED 列出 LLM provider 档案）。
- **任务分支名 = 任务 slug** `provider-per-shape-urls`（与 backup-feature 先例一致）。

## Risks / Trade-offs

- core 的 switch 行为变更涉及回归面（六个 agent 适配器）：core tasks 已含「未设形态地址行为与现状一致」回归项，收尾段再做全仓回归兜底。
- ADR 编号 0027 假设归档时无其他新 ADR 占号：晋升时若冲突顺延并更新互链。

## Open Questions

- 无（探索任务 frontier 已清空）
