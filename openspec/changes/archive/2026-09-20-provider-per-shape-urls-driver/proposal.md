## Why

为一个 provider 提供按接口形态（openai chat / openai responses / anthropic）各自的接入地址；某形态地址未设置时，按既有规则从 `BaseURL` 自动推断。

背景：ADR-0004「一份档案一个接入地址」把 per-形态覆盖留作逃生舱（「等真出现第二个不兼容端点再加」）；真实网关三端点不共根（chat 与 responses 也可能不同根），本 change 兑现该逃生舱，粒度开到 per-shape（per-agent 覆盖仍不做）。

已冻结方案（探索任务 `provider-per-shape-urls`）：`BaseURL` 保留必填（语义收窄为默认地址 + 推断源），新增三个可选字段 `chat_base_url` / `responses_base_url` / `anthropic_base_url`；门禁放宽为「目标协议族存在显式形态地址即放行」；地址选择严格跟随线协议（形态地址存在性从不牵引协议选择）；`anthropic_base_url` 原样存储仅收敛尾斜杠；存量档案零迁移。

产品决策与规则明细见归档探索任务：`tasks/archive/2026-09-19/provider-per-shape-urls/`（`TASK.md`、`design/adr-per-shape-urls.md`、`glossary.md`）。

实现对照：`internal/storage/types.go`（`LLMProviderEntry`）、`internal/llm/baseurl.go`、`internal/llm/apishape.go`、`internal/llm/switch.go`、`cmd/ai_provider.go`、`internal/tui/`（AI Tab）、`cmd/mcp_llm.go`（`llmProviderView`）。

## What Changes

- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals

- per-agent 地址覆盖（agent 矩阵不进 vault schema）
- 从 URL 内容推断形态（ADR-0006 明确拒绝的路径）
- 允许省略 `BaseURL`（即使三个形态地址全配）
- 存量档案迁移（字段缺省即旧行为）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准

- [x] `LLMProviderEntry` 新增可选字段 `chat_base_url` / `responses_base_url` / `anthropic_base_url`；URL 校验与 `--allow-http` 门禁覆盖三字段；存量档案零迁移
- [x] 归一化：OpenAI 族字段沿用现规则（补末段版本段、收敛尾斜杠）；`anthropic_base_url` 原样存储、仅收敛尾斜杠
- [x] 形态地址未设置时 switch 行为与现状一致（从 `BaseURL` 按协议族推断）
- [x] switch 门禁：目标族有显式形态地址即放行；无显式地址时维持 `api_shape` 旧判定；拒绝文案给三个可行动作（改档案形态 / 补 `--shape-url` / 换 provider）
- [x] 地址选择严格跟随线协议（`api_shape` 或 agent 既有默认决定 W，地址跟随 W）；`switch` 输出实际写入地址与来源（explicit / inferred）
- [x] CLI `ai provider add/edit` 支持重复 flag `--shape-url <api_shape>=<url>`（传值设置、空值清空、edit 省略的 key 保留）；`show`/`list` 展示形态地址
- [x] TUI provider 表单新增三个形态地址字段，详情弹层展示形态地址
- [x] MCP `llm_provider_list` 视图新增 `shape_urls`（`{chat?, responses?, anthropic?}`，omitempty）与 `api_shape`
- [x] `.agents/skills/senv-cli/SKILL.md` 回写；仓库 `docs/adr/` 晋升 per-shape ADR 并标注与 ADR-0004/0006 的修订关系
- [x] 全仓回归（`go test ./...`）与 `openspec validate --strict` 通过

## Driver 协议

- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `provider-per-shape-urls-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/provider-per-shape-urls-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-19 propose：拆 2 个子 change（core / surfaces）；driver `skip_specs: true`；三个 change `openspec validate --strict` 待勾后随 apply 复验。
- 2026-09-20 apply：core / surfaces 两个子 change tasks 全勾且 `openspec validate --strict --type change` 均通过。回归：`go build ./...`、`go test ./...` 全绿；`golangci-lint v1.64.8 run --new-from-rev=origin/main` 新代码 0 issue（本机无预装，临时 `go install` 到 /tmp；CI 只跑 go test）；`gofmt` 通过（help.go/ssh.go 为 main 既有未格式化文件，未触碰）。`go run . --help`、`go run . ai provider add/edit --help`、`go run . mcp list-tools` 语法核对通过。