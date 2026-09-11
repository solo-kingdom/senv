# Design: tui-ux-forms

## Context

`form.go` 引擎已覆盖 text/secret/enum/ref/path/editor 五类字段与 reopen 回填；config 创建向导（`configModeCreateName→Source→Target→Group→Desc`）与 env 新建（key/value 连续弹窗）是仅存的手搓流程。无新架构决策，属契约对齐。

## Goals / Non-Goals

**Goals:** 两处流程迁移；env value 升级为遮蔽输入（安全姿态与 AI/MCP 凭据录入对齐）。

**Non-Goals:** 不改 form 引擎本身；不动 text vim 闭环；不新增字段类型。

## Decisions

- **config 表单字段与校验**：name（必填 + 重名冲突校验）、源文件路径（必填 + 存在性校验，`formPath`）、target 路径（必填）、分组（`formRef` 既有分组 + 允许空 = default）、描述（可选）。提交失败走 `mcpFormReopenMsg` 同款 reopen 模式，错误映射回字段。
- **env value 用 `formSecret`**：新建场景无预填需求，遮蔽不损失体验；内联编辑（`e`）保持现状——已值遮蔽编辑需要「留空保持原值」语义，超出本 change，不做。此为有意的范围切分，记录于此。
- **删除手搓代码而非并存**：向导状态机（`configModeCreate*`）整段移除，避免双路径漂移。

## 数据流与错误处理

表单提交调用既有 `config.Manager.Create` / `env.Manager.Set`，失败回显表单不落盘；成功后快照失效重建（既有机制），列表刷新。secret 字段值仅存在于表单提交路径，MUST NOT 进入渲染文本/日志（沿用 tui-forms 契约）。

## Risks / Trade-offs

- [5 步向导用户依赖逐步引导] → 表单单屏可见全部字段，字段数少（≤5），不构成认知负担
- [env 明文值改为遮蔽后无法目视核对] → 提交前可经 `v` 语义在列表侧核对（既有遮蔽/揭示机制），不在表单内重复造揭示

## Open Questions

无。
