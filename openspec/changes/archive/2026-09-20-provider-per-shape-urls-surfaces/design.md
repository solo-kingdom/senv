# Design

## Context

core 切片已为档案增加三个形态地址字段并打通 CLI；本切片把同一能力补齐到 TUI、MCP 与文档面。TUI provider 表单/详情与 MCP 视图均为既有实现（`internal/tui/` AI Tab、`cmd/mcp_llm.go` `llmProviderView`），只需增量扩展。

## Goals / Non-Goals

**Goals:**

- TUI 表单与详情覆盖三字段
- MCP 视图补 `api_shape` 与 `shape_urls`
- SKILL.md 回写、仓库 ADR 晋升

**Non-Goals:**

- 数据面与 CLI 行为（core 已覆盖）；MCP 仍只读，不新增写工具

## Decisions

- **TUI 表单为三个固定字段**（非动态控件）：shapes 是封闭枚举；编辑表单用既有值预填，清空提交等价显式清空（与既有元数据清空语义一致）；新建时留空 = 不设置。
- **MCP 视图平铺进白名单**：`llmProviderView` 增加 `APIShape string \`json:"api_shape,omitempty"\`` 与 `ShapeURLs *llmProviderShapeURLs \`json:"shape_urls,omitempty"\``（三键各自 omitempty，全空不出现在响应）；从 `storage.LLMProviderEntry` 直接映射，凭据字段仍结构性排除。
- **ADR 晋升编号顺延为 0027**，Status 标注修订 ADR-0006（形态不再只由声明决定，显式形态地址参与门禁）并细化 ADR-0004（单地址原则收窄为「默认地址 + 可选形态地址」）；0004/0006 的 Status 节互链到 0027。
- **SKILL.md 整体在本切片一次回写**（不随 core 拆改）：provider add/edit、switch、TUI AI Tab、MCP 段一次对齐，避免两个子 change 改同一文件。

## Risks / Trade-offs

- MCP 暴露面扩大（新增两字段）：均为档案元数据，与 `base_url` 同级；白名单模式保证不泄漏其它字段。
- SKILL.md 与实现漂移风险：回写后用 `go run . --help` / `go run . ai provider --help` / `go run . mcp list-tools` 核对。

## Open Questions

- 无
