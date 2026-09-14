## Why

`senv ai switch codex` 写入的 `env_key` 是按 alias 派生的 `SENV_<ALIAS>_API_KEY`，但没有任何 senv 命令会产生这个名字（`senv env export` 只导出 vault 里存的名字），于是 spec 承诺的补救路径「用 senv env 能力把被引用 key 暴露给 codex」不可执行：用户的配置完全合理（`env:ai/DEEPSEEK_API_KEY`，组已激活、shell 已在导出），codex 仍报 `Missing environment variable`；TUI 默认的 `[new own credential]`（`text:llm-keys/<alias>`）更是必然不可满足。codex 还是唯一不解密凭据的适配器——凭据条目缺失时切换照样成功，静默产出坏配置（违背 ADR-0019 的 fail-closed）。

## What Changes

- codex 的 `env_key` 优先取 `credential_ref` 里的 env key 名：`env:<g>/<k>` → `env_key = <k>`，与 shell 中 `senv env export` 已提供的名字一致，零额外操作。
- `text:` 引用（含自有凭据 `text:llm-keys/<alias>`）保留派生名 `SENV_<ALIAS>_API_KEY`，并由切换在**默认 env 组**写入引用条目 `SENV_<ALIAS>_API_KEY = {{text:<g>/<k>}}`，使该名字可被导出；同名条目已存在时一律不覆盖。
- 引用所属 env 组不在导出集合（默认组 ∪ 已激活组）内时给出可操作 warning（`senv env group activate <g>`）。
- codex 切换同样解密凭据，以复用 ADR-0019 的凭据缺失 fail-closed 诊断（明文不落盘，只用其存在性与名字）。
- CLI 与 TUI 的切换提示改为给出确定的环境变量名与可复制命令；TUI 展示全部 warning 而非仅第一条。
- 同步更新 `.agents/skills/senv-cli/SKILL.md`，并新增 ADR 记录该命名决策。

## Capabilities

### Modified Capabilities

- `llm-provider-switch`: codex 凭据暴露名的选择规则（env: 复用引用 key 名；text: 派生名 + 默认组引用条目兜底）、组未激活的可操作提示、codex 的凭据缺失 fail-closed。
- `llm-provider-tui`: 切换成功提示携带确定的环境变量名与全部 warning。

## Non-goals

- 不给 codex 注入进程环境，不引入启动包装或凭据代理（沿用既有边界）。
- 不改 `senv env export` 的输出语义（不新增按 alias 派生名的别名导出）。
- 不改其他 agent 的凭据模式，不改本机指针模型与 MCP 工具契约。

## 安全性分析

- codex 的明文 key 仍不落 agent 配置：`env:` 引用只是改用 vault 里已有的名字；`text:` 引用落盘的是 `{{text:...}}` 模板，明文仅在 `senv env export` 解密时进入 shell，与既有 env 组暴露面相同。
- 兜底写入不覆盖用户已有的同名条目，走既有 env manager（0600、事务与审计随 switch）。

## Impact

- 代码：`internal/llm/switch.go`（env_key 决议、兜底写入、组检查）、`cmd/ai_switch.go`、`internal/tui/ai_tab.go`。
- 测试：`internal/llm/switch_test.go`、`internal/llm/credential_ref_test.go`、`cmd/ai_switch_test.go`、`internal/tui/ai_tab_test.go`。
- 行为：codex `config.toml` 的 `env_key` 取值变化（`env:` 引用不再被改写为 `SENV_...`）；切换可能新增一条默认组 env 引用条目（幂等 vault 写入）。
- 文档：`.agents/skills/senv-cli/SKILL.md`、`docs/adr/`。
