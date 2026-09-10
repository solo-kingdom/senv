## Why

当前 `senv ai switch <agent> <provider>` 只把**单个**模型写进 agent 配置：claude-code 的 `model`、codex 的 `model`、kimi 的单条 `[models.*]`、pi 的单元素 `models[]`、opencode 的单个 `models` 键。结果是 provider 档案里的模型集在切换后完全不可用，用户在 agent 里换模型必须回到 senv。但「切换」在用户心智里是换 provider：模型集与默认模型应当一并落进 agent，由 agent 自己的模型选择器承担集合内切换。

## What Changes

- 本 change 是 taskflow driver，不直接改代码，只编排子 change
- `senv ai switch` 语义改为：agent → provider 指向 + **Agent 模型集**（Provider 模型集的子集，默认全选）+ **默认模型**
- 各 agent 按其原生机制投影模型集：claude-code `modelPicker`、codex `model_catalog_json`、kimi 多条 `[models.*]`、pi `models[]`、opencode `models{}`
- 本机指针扩展为 provider + Agent 模型集 + 默认模型；v1 旧指针读作单元素集，无需重新切换
- 切换时清理自己上一次写入的差集条目与失效 catalog 文件；用户自有配置键不动
- CLI/TUI 表面同步变化：新增 `--models`/`--default-model`，移除 `--model`，TUI 改多选

## Non-goals

- 不做 LLM 请求代理/转发，senv 仍只负责档案管理与配置写入
- 不在 agent 内新增 senv 自己的模型选择器（复用 agent 原生 picker）
- 不把 Agent 模型集持久化进 vault：它仍是本机状态
- 不改 LLM Provider 档案 schema、凭据引用语义与接入地址归一规则（ADR-0004/0006 不变）
- 不支持 project 级切换
- 不自动回写：provider 档案变更后不自动重写 agent 配置，只在下一次显式切换时重算
- 不新增 unswitch/回滚命令

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准

- [x] `senv ai switch <agent> <provider>` 省略 `--models` 时把 Provider 模型集全量写入 agent 配置，且该 agent 的原生模型选择器能在集合内切换
- [x] `--models m1,m2` 只写入该子集（保序），非法值报错并列出候选；空集报错；`--model` 报错并提示改用 `--models`/`--default-model`
- [x] 默认模型默认取 provider 档案的默认模型；`--default-model` 只覆盖本次且不回写档案；档案无默认模型且未指定时报错
- [x] 五个 agent 的投影落地：claude-code `modelPicker` + `replaceBuiltInOptions: true`、codex 生成 `~/.codex/model-catalogs/senv-<alias>.json`、kimi 多条 `[models."senv-<alias>/<m>"]`、pi `providers.<id>.models[]`、opencode `provider.<id>.models{}`
- [x] kimi 的 `max_context_size` 优先取 models.dev `limit.context`，缺失时回退保守值
- [x] 切换清理上一次 senv 写入的差集条目与不再指向的 `senv-*.json` catalog；用户自有键保留（备份语义见验证记录）
- [x] 指针 v1 兼容：旧指针读作单元素集；`senv ai status` 显示 `provider / 默认模型（N 个模型）` 与漂移提示
- [x] TUI AI tab 支持模型集多选（默认全选）与默认模型选择；`m` 仅在现有 Agent 模型集内换默认模型
- [x] `.agents/skills/senv-cli/SKILL.md` 同步更新；`make check` 通过

## Driver 协议

- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `{task}-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/{task}-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-11 grill：11 项决策 settled（见 `grill.md`），两条 ADR 已记入 `docs/adr/0011`、`docs/adr/0012`（proposed）。
- 2026-09-11 子 change 落地：`agent-model-set-store`、`agent-model-set-agents`、`agent-model-set-cli`、`agent-model-set-tui` 全部 checkbox 勾选、`validate --strict` 通过；各 change 的 proposal 验证记录含单测明细。
- 2026-09-11 全仓回归：`make check`（`go fmt ./...` + `go vet ./...` + `golangci-lint run ./...` + `go test -race ./...`）全绿。
- 2026-09-11 备份措辞：验收标准里的「覆盖前仍留 `.senv-bak`」按子 change spec 统一为「事务期间持有每路径私密备份、成功后清理」，由 `TestConfigTransactionWritesPrivatelyAndCleansBackups` 与 `TestSwitchRollsBackNewCatalogOnConfigFailure` 覆盖；agent 配置不再遗留常驻 `.senv-bak`。
- 2026-09-11 交付：用户确认「一块提交」，本 task 改动与同一工作树内另一任务（session 到期模型/绑定身份）的改动合并为一次提交；随后按 `openspec archive` 依次归档 4 个子 change（delta spec 已合并进 `openspec/specs/llm-provider-switch`、`openspec/specs/llm-provider-tui`），最后归档本 driver。
