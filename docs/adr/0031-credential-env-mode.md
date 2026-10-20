# 0031-credential-env-mode

`senv ai switch` 新增 `--credential-mode=auto|env|inline`（默认 auto），把 ADR-0002 的「一律写明文」收窄为「agent 支持环境变量反解时优先写引用，不支持时才写明文」。ADR-0002 的前提是「agent 自身无法从 vault 拉凭据，MCP 无法在请求链路注入认证」——调研发现该前提对多数 agent 已不成立：客户端配置普遍支持环境变量引用，明文落盘不再是唯一选择。

auto 模式下，adapter 声明了反解能力（`CredentialEnvRef` 非 nil）即走 `CredentialEnvVar`（只写环境变量引用，明文由 `senv env export` 注入 shell），否则回落 `CredentialInline`（ADR-0002 原行为）。`--credential-mode=env` 强制引用，adapter 不支持时报错；`--credential-mode=inline` 强制明文，与旧行为逐字节一致。各 agent 的反解语法（2026-10-20 官方文档核实）：

| Agent | 语法 | 出处 |
|-------|------|------|
| codex | `env_key = "VAR"`（ADR-0024 已落地） | OpenAI Codex config |
| kimi | `api_key_env = "VAR"`（与 `api_key` 互斥，每次请求重读） | moonshotai kimi-code docs |
| pi | `apiKey = "$VAR"` / `"${VAR}"` 插值 | badlogic/pi-mono models.md |
| omp | `apiKey = VAR`（先按 env 解析，未设/为空 fallback 字面量） | can1357/oh-my-pi models.md |
| opencode | `apiKey = "{env:VAR}"` | opencode.ai/docs/providers |
| claude-code | settings.json 不展开 `${VAR}`（env 块值是字面量）；缺键时继承进程环境 | code.claude.com/docs/env-vars |

claude-code 特殊：settings.json 的 env 块不反解插值，因此不能写 `"$VAR"` 形式的引用。auto 模式下 claude-code 的 `CredentialEnvRef` 声明为「支持但引用串为空」——真正的行为差异在 adapter 里：env 模式**不写** `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN`，由进程环境兜底（Claude Code 启动时 settings 优先、缺键才继承进程环境，2026-10 实测）。由于 base URL 也不落盘，`CredentialEnvBaseURL` 让 SwitchManager 同时决议 `SENV_<ALIAS>_API_KEY_BASE_URL` 环境变量（值 = 解析后的接入地址）并保证可由 `senv env export` 提供。代价：用户必须先在 shell 里 `eval $(senv env export)` 再启动 claude，否则 agent 起不来——因此 claude-code 的 env 模式只在用户显式 `--credential-mode=env` 时启用，auto 回落 inline 保持「无 senv session 也能起」。env 模式切换会 delete 上次 inline 切换留下的 `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` 键，避免 stale 明文残留。

env 名的决议复用 ADR-0024 的机制（泛化为 `credentialEnvPlanFor` / `ensureCredentialEnvName`）：`env:<g>/<k>` 引用复用 `<k>`（shell 里既有名字）；`text:<g>/<k>` 引用派生 `SENV_<ALIAS>_API_KEY` 并在默认组补引用条目。claude-code 的 base URL env 用同一机制，SeedRef 直接是解析后的 URL（明文是公开地址，非凭据）。

## Considered Options

- **默认全改 env（甲）**：彻底消灭明文，但 claude-code 失去「无 senv session 也能起」；kimi/pi/omp 用户 shell 里必须有 `senv env export` 求值，否则 agent 报 Missing env——失败面比 inline 大，多数用户不会主动选择。
- **`--credential-mode=env|inline` 开关、默认 inline（乙保守版）**：零行为破坏，但默认仍写明文，「优先环境变量」目标落空。
- **不改 senv（丙）**：已被调研证伪——五个 agent 都有原生反解机制，「只能写明文」的前提不成立。
- **采用「乙为骨架 + 甲为默认值」**：`--credential-mode` 三档，默认 auto = 支持 env 的 agent 走引用、claude-code 走 inline。既兑现「优先环境变量」，又把 claude-code 的可用性损失交还用户显式选择（`--credential-mode=env`）。

## Consequences

- `AgentAdapter` 的 `Credential CredentialMode` 字段（硬编码）被 `CredentialEnvRef func(name string) string` + `CredentialEnvBaseURL bool` 替代；模式由 `Switch()` 的 `credentialMode` 参数 + adapter 能力在切换时决议，不再编译期钉死。
- `Switch()` 签名新增 `credentialMode CredentialModePreference` 参数；TUI 向导在后台模型步骤之后新增凭据模式步骤（auto/env/inline 三选一，默认 auto；仅换默认模型的 `M` 不出现该步骤，凭据写回不发生在仅换模型路径）。
- ADR-0002 的「有意取舍」收窄为「仅对不支持反解的 agent 写明文」；凭据代理仍属 Non-goal。
- `codexEnvPlanFor` / `ensureCodexEnvName` 泛化为 `credentialEnvPlanFor` / `ensureCredentialEnvName`（文案参数化 agent 名），codex 测试不改语义。

## Status

accepted
