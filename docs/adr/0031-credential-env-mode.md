# 0031-credential-env-mode

`senv ai switch` 新增 `--credential-mode=auto|env|inline`（默认 auto），把 ADR-0002 的「一律写明文」收窄为「agent 支持环境变量反解时优先写引用，不支持时才写明文」。ADR-0002 的前提是「agent 自身无法从 vault 拉凭据，MCP 无法在请求链路注入认证」——调研发现该前提对多数 agent 已不成立：客户端配置普遍支持环境变量引用，明文落盘不再是唯一选择。

auto 模式下，adapter 必须同时满足「声明了反解能力」（`CredentialEnvRef` 非 nil）与「能为探针名给出非空引用串」才走 `CredentialEnvVar`（只写环境变量引用，明文由 `senv env export` 注入 shell），否则回落 `CredentialInline`（ADR-0002 原行为）。非 nil 只表示支持显式 env 模式，非空才表示 auto 可用：`settings.json` 这类「写不进引用」的配置形态给了能力也无法在 auto 下自洽。`--credential-mode=env` 强制引用，adapter 不支持时报错；`--credential-mode=inline` 强制明文，与旧行为逐字节一致。各 agent 的反解语法（2026-10-20 官方文档核实）：

| Agent | 语法 | 出处 |
|-------|------|------|
| codex | `env_key = "VAR"`（ADR-0024 已落地） | OpenAI Codex config |
| kimi | `api_key_env = "VAR"`（与 `api_key` 互斥，每次请求重读） | moonshotai kimi-code docs |
| pi | `apiKey = "$VAR"` / `"${VAR}"` 插值 | badlogic/pi-mono models.md |
| omp | `apiKey = VAR`（先按 env 解析，未设/为空 fallback 字面量） | can1357/oh-my-pi models.md |
| opencode | `apiKey = "{env:VAR}"` | opencode.ai/docs/providers |
| claude-code | settings.json 不展开 `${VAR}`（env 块值是字面量）；缺键时继承进程环境 | code.claude.com/docs/env-vars |

claude-code 特殊：settings.json 的 env 块不反解插值，因此不能写 `"$VAR"` 形式的引用。它的 `CredentialEnvRef` 声明为非 nil 但返回空串——非 nil 只为放行显式 `--credential-mode=env`，空串让 auto 判定落到 inline（见上）。显式 env 模式下，senv 用 Claude Code 只认的契约变量名，而不是 `SENV_<ALIAS>_API_KEY`：

- 凭据条目固定 `ANTHROPIC_AUTH_TOKEN`，值是该档案凭据的 senv 引用——`text:<g>/<k>` → `{{text:<g>:<k>}}`，`env:<g>/<k>` → `{{env:<g>:<k>}}`（**不**复用被引用的 key 名当变量名，claude-code 不认它）。
- 接入地址条目固定 `ANTHROPIC_BASE_URL`，值是解析后的地址字面量（地址是公开信息，非密钥）。
- 两条都写进默认 env 组，settings.json 两个键都不写，由进程环境兜底（Claude Code 启动时 settings 优先、缺键才继承进程环境，2026-10 实测）。代价：用户必须先在 shell 里 `eval $(senv env export)` 再启动 claude，否则 agent 起不来——因此 claude-code 的 env 模式只在用户显式 `--credential-mode=env` 时启用，auto 回落 inline 保持「无 senv session 也能起」。

归属与更新规则（`SetWithDescription` 写归属标记，读取走 `GetWithMeta` / `ListVarInfo` 的描述字段）：

- 两条条目分别带固定 description：`senv-managed: claude-code env-mode credential` / `senv-managed: claude-code env-mode base URL`。标记即「senv 拥有」，反复切 provider 时直接覆盖值并保留标记。
- 同名条目已存在但无标记、且解析值等于本次凭据/地址 → 视为可用，保持不动（用户等价条目）；值不匹配 → fail closed，报可行动错误（说明名字被占用、可 `senv env delete` 或改名或改用 inline），零写盘。
- 同名条目只存在于未激活组 → 也 fail closed（导出集合不含它，静默成功等于留下不可用状态），提示 `senv env group activate <g>` 或删除后重试。
- 相反，其他 agent 的名字只存在于未激活组、或已存在且指向别的凭据时仍是 warning（ADR-0024 起的既有行为），因为那些 agent 的名称决议沿用 `<k>` / `SENV_<ALIAS>_API_KEY`，用户可能有意覆盖。
- 切到非 env 模式（inline/auto）时，切换事务成功提交后 best-effort 删除带标记的两条条目；无标记的用户条目一律不动，历史遗留的 `SENV_<ALIAS>_API_KEY(_BASE_URL)` 也不在清理范围（那是旧版本写入的派生名，与本契约无关）。清理失败只追加 warning，不影响已成功的切换结果。

env 名的决议对其它 agent 仍复用 ADR-0024 的机制（`credentialEnvPlanFor` / `ensureCredentialEnvName`）：`env:<g>/<k>` 引用复用 `<k>`（shell 里既有名字）；`text:<g>/<k>` 引用派生 `SENV_<ALIAS>_API_KEY` 并在默认组补引用条目。

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

## 2026-10-27 修订

两处实现与原文不符，按实测收敛：

1. **auto 判据收紧**：原文「`CredentialEnvRef` 非 nil 即走 env」对 claude-code（桩返回空串）错误地选了 env，导致默认切换后 settings.json 既无 `ANTHROPIC_BASE_URL` 也无 `ANTHROPIC_AUTH_TOKEN`，claude-code 启动报 `Not logged in · Please run /login`。现改为「非 nil 且探针名能返回非空引用串」才走 env；非 nil 仅表示支持显式 env 模式。codex/kimi/pi/omp/opencode 的 auto 行为不变。
2. **claude-code 的 env 名改为契约名**：原文的 `SENV_<ALIAS>_API_KEY` / `SENV_<ALIAS>_API_KEY_BASE_URL` 是 senv 自造名，claude-code 只读 `ANTHROPIC_AUTH_TOKEN` / `ANTHROPIC_BASE_URL`，`eval "$(senv env export)"` 永远兜不住。现改为两条固定契约名条目、写进默认 env 组、带 `senv-managed:` description 归属标记：带标记直接覆盖（反复切 provider 幂等）、无标记但解析值相同视为可用、无标记且值冲突或只存在于未激活组一律 fail closed（零写盘、给出 `senv env delete` / `env group activate` 指引）、切回 inline/auto 后 best-effort 删除带标记条目。历史遗留的 `SENV_<ALIAS>_API_KEY(_BASE_URL)` 不在清理范围。

代码落点：`autoCredentialEnvAvailable`、`claudeCodeCredentialEnvRef`、`ensureClaudeCodeEnvEntries` / `planClaudeContractEnvEntry`、`cleanupClaudeCodeEnvEntries`（`internal/llm/switch.go`）。
