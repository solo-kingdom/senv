## Why

per-shape 形态地址的交互面与文档：core 切片落地了 schema、switch 与 CLI，但 TUI 表单、MCP 视图与 agent 文档不同步就违反「CLI 与 TUI 两个一等交互面同步评估」与「用户可见命令变更必须回写 senv-cli skill」的产品约定。

编排见 `provider-per-shape-urls-driver`。产品决策见归档探索任务 `tasks/archive/2026-09-19/provider-per-shape-urls/`（`design/adr-per-shape-urls.md`）。

实现对照：`internal/tui/`（AI Tab provider 表单与详情）、`cmd/mcp_llm.go`（`llmProviderView`）、`.agents/skills/senv-cli/SKILL.md`、`docs/adr/`。

## What Changes

- TUI AI Tab provider 表单（新建/编辑）新增三个形态地址字段：`chat_base_url` / `responses_base_url` / `anthropic_base_url`；编辑表单用既有值预填，清空提交 = 清除该字段；详情弹层展示形态地址
- MCP `llm_provider_list` 白名单视图新增 `api_shape` 与 `shape_urls`（`{chat?, responses?, anthropic?}`，omitempty）；仍不含凭据明文
- `.agents/skills/senv-cli/SKILL.md` 回写：provider add/edit `--shape-url`、switch 门禁放宽与输出来源、anthropic 地址原样存储约定、TUI 表单字段、MCP 视图字段
- 仓库 `docs/adr/` 晋升 per-shape ADR（新编号顺延，标注与 ADR-0004/0006 的修订关系，0004/0006 Status 互链）
- specs：delta `llm-provider-tui`、`llm-provider-mcp`

**安全性分析**：TUI 表单与详情只展示地址（非密钥）；MCP 白名单扩展字段均为档案元数据，凭据明文仍结构性排除。

## Non-goals

- schema / 归一化 / switch 门禁与地址解析 / CLI flag（`provider-per-shape-urls-core`）
- per-agent 覆盖、URL 内容推断、省略 `BaseURL`、存量迁移（driver Non-goals）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 由 driver 准备段切分支 |

## 验收标准

- [ ] TUI provider 表单三字段可用（新建/编辑/清空语义正确），详情弹层展示形态地址
- [ ] MCP `llm_provider_list` 返回 `api_shape` 与 `shape_urls`，无凭据明文
- [ ] `senv-cli/SKILL.md` 与实际命令行为一致（`go run .` 核对）
- [ ] 仓库 ADR 晋升完成且 0004/0006 互链
- [ ] `go test ./...` 全绿，新代码 lint 0 issue
