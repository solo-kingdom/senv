## Why

per-shape 形态地址的数据面与 CLI：单一 `BaseURL` 覆盖不了三端点不共根的真实网关（chat 与 responses 也可能不同根）——这是 ADR-0004 预留逃生舱的触发条件。本切片落地 vault schema 扩展、归一化分族、switch 门禁放宽与地址选择、CLI `--shape-url` 与 `show`/`list` 展示。TUI/MCP/文档见后续切片。

编排见 `provider-per-shape-urls-driver`。产品决策与规则明细见归档探索任务 `tasks/archive/2026-09-19/provider-per-shape-urls/`（`design/adr-per-shape-urls.md`）。

实现对照：`internal/storage/types.go`（`LLMProviderEntry`、`ValidateLLMProvider`）、`internal/llm/baseurl.go`、`internal/llm/apishape.go`、`internal/llm/switch.go`、`cmd/ai_provider.go`。

## What Changes

- `LLMProviderEntry` 新增三个可选字段 `chat_base_url` / `responses_base_url` / `anthropic_base_url`（omitempty；空 = 未声明）；存量档案零迁移
- URL 校验与 `--allow-http` 门禁覆盖三字段（同 `BaseURL` 既有规则：HTTPS 默认、拒绝 userinfo 与空 host）
- 归一化分族：OpenAI 族字段沿用现规则（补末段版本段、收敛尾斜杠、纯数字版本段视为已归一）；`anthropic_base_url` 原样存储、仅收敛尾斜杠
- `senv ai switch` 门禁放宽：目标协议族存在显式形态地址即放行（Anthropic 族看 `anthropic_base_url`；OpenAI 族看 `chat_base_url`/`responses_base_url` 任一）；无显式地址时维持 `api_shape` 旧判定；拒绝文案扩为三个可行动作
- 地址选择严格跟随线协议 W：Anthropic 族（claude-code）用 `anthropic_base_url` 原样写回，未设回落既有推断；OpenAI 族由 `api_shape`（openai-* 声明）或 agent 既有默认决定 W，地址 = W 对应形态字段，未设回落 `BaseURL`
- `switch` 成功输出含实际写入地址与来源（explicit / inferred）
- CLI `ai provider add/edit` 新增重复 flag `--shape-url <api_shape>=<url>`（传值设置、空值清空、edit 省略的 key 保留）；`show`/`list` 展示形态地址
- specs：delta `llm-provider`、`llm-provider-switch`

**安全性分析**：形态地址与 `BaseURL` 同为非密钥 URL，同受 HTTPS / `--allow-http` 门禁；不新增凭据暴露面；switch 输出的来源标注不含凭据材料。

## Non-goals

- per-agent 地址覆盖（agent 矩阵不进 vault schema）
- 从 URL 内容推断形态（ADR-0006 拒绝的路径）
- 允许省略 `BaseURL`（即使三个形态地址全配）
- 存量档案迁移
- TUI 表单/详情、MCP 视图、SKILL.md 回写与 ADR 晋升（`provider-per-shape-urls-surfaces`）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 由 driver 准备段切分支 |

## 验收标准

- [ ] `LLMProviderEntry` 三可选字段落库；URL 校验与 `--allow-http` 门禁覆盖三字段；存量档案读取不受影响
- [ ] 归一化分族生效：OpenAI 族字段沿用现规则；`anthropic_base_url` 原样仅收敛尾斜杠；非法 URL（空 host、userinfo、非允许 HTTP）拒绝
- [ ] 形态地址未设置时 switch 行为与现状完全一致（回归）
- [ ] 门禁放宽与三动作拒绝文案按规则生效
- [ ] 地址选择严格跟随线协议；`switch` 输出实际写入地址与来源
- [ ] CLI `--shape-url` 的设置/清空/保留语义与 `show`/`list` 展示生效
- [ ] `go test ./...` 全绿，新代码 lint 0 issue
