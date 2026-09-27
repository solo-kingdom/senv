## Context

senv 的 coding agent 支持分两个注册表：`internal/agentcfg`（mcp install/export 目标，`Target` 结构）与 `internal/llm`（切换适配器，`AgentAdapter` 结构 + `applyJSONMerge`/`applyTOMLMerge` 两个写回原语 + `configTransaction` 事务协议）。两处清单都被 CLI 与 TUI 共用，新增 agent 即注册表加条目。

omp（can1357/oh-my-pi，本机 v18.3.3 实测）是 Pi 的 fork，但配置面已分化：MCP 内置（`~/.omp/agent/mcp.json`、project `.omp/mcp.json`，remote 条目必须写 `type` 键）；provider 配置为 `~/.omp/agent/models.yml`（YAML，非 pi 的 models.json）；默认模型为 `~/.omp/agent/config.yml` 的 `modelRoles.default`（`<provider>/<model>` 选择子）。路径解析规则见 `packages/utils/src/dirs.ts` 与 docs（config-usage.md、mcp-config.md、models.md、environment-variables.md）。动机与取舍见 proposal.md 与 `docs/adr/0030-oh-my-pi-agent-yaml-writeback.md`。

## Goals / Non-Goals

**Goals:**
- `agentcfg` 新增 `omp` 目标（含 `OmpAgentDir` 路径解析与别名机制），install/export/TUI 自动跟进。
- `llm` 新增 `ompAdapter`：写 `models.yml` provider 条目 + `config.yml` modelRoles.default；新增 `applyYAMLMerge` 原语纳入同一事务协议。
- 两个注册表支持 `Aliases` 查找（omp ← `oh-my-pi`），展示面不变。

**Non-Goals:**
- 不支持 XDG 重定向、不写 `modelRoles` 其它角色、不用 env 名/`!cmd` 间接凭据、不改 pi 目标行为、不动 codex/claude-code/kimi/opencode 适配器。

## Decisions

1. **`OmpAgentDir` 完全镜像 omp 的 dirs.ts**：profile（`OMP_PROFILE` 优先于 `PI_PROFILE`，名字不合法时回退默认——与 `readProfileFromEnvSafe` 一致）→ `PI_CONFIG_DIR`（配置根，`~`/`~/...` 展开）→ `PI_CODING_AGENT_DIR`（仅默认 profile）。镜像而非抽象通用层：pi 与 omp 的 env 规则未来可能再分化，过早抽象会把两个上游锁进同一函数。profile 合法性校验直接抄 omp 的正则与保留名规则，附注释给出源码锚点。
2. **`applyYAMLMerge` 与 JSON/TOML 原语并列**：同样签名（path + mutate(root map[string]any) + txs...），yaml.v3 解码为 `map[string]any`，mutate 后编码写回。注释丢失接受（同 TOML 先例，见 ADR-0030）。`gopkg.in/yaml.v3` 从 indirect 转直接依赖（go.mod 调整）。
3. **omp 适配器复用 pi 的投影代码**：models.yml 的 provider/model 条目字段与 pi 的 models.json 同 schema（`baseUrl`/`apiKey`/`api`/`compat`/`models[]`，模型级 `id`/`name`/`contextWindow`/`maxTokens`/`reasoning`/`input`），把 pi 适配器里构造 provider 条目与模型条目的逻辑抽成共享函数（pi 与 omp 共用），`compat.supportsDeveloperRole: false` 的理由注释保留并指向 pi 的那一份。`piAPIType` 直接复用（omp 的 `api` 取值集相同）。差异只在落点：pi 写 models.json + settings.json enabledModels 重排；omp 写 models.yml + config.yml `modelRoles.default`，无 enabledModels 概念。
4. **别名机制通用化**：`Target` 与 `AgentAdapter` 各加 `Aliases []string`，`Find`/`LookupAgent` 遍历匹配（大小写不敏感），错误信息、列表、`ai status` 仍走 `ID`。不引入注册表级 alias map，避免第三处状态。
5. **`omp` 目标无 `Prerequisite`**：omp 内置 MCP，与 cursor/zcode 同列；`Remote` = {HTTP: true, SSE: true, Headers: true, TypeKey: true}，Reason 注明 sse 已弃用、新配应以 http 为主。project scope 返回相对路径 `.omp/mcp.json`（同 cursor 模式）。
6. **凭据 CredentialInline**：解密明文写 `models.yml` 的 `apiKey`，文件收敛 0600（与 pi、ADR-0002 一致）。`senv ai status` 的配置路径展示走 adapter 的 `ConfigPath`，自动正确。

## Risks / Trade-offs

- [omp 上游演进快，YAML schema/profile 规则可能漂移] → 所有镜像逻辑附源码/文档锚点注释（版本 v18.3.3）；skill 文档记录验证锚点；smoke 用本机真实 omp。
- [yaml.v3 重写丢用户注释] → 与 TOML 先例一致，ADR-0030 记录；不在输出加警告（用户无能为力，只会成噪音）。
- [临时 HOME 冒烟漏掉 XDG/平台差异] → 冒烟只验证配置被解析加载（启动即报 schema 错误 vs 进入网络调用），不追求端到端模型调用；平台相关路径解析由单测覆盖。
- [`Aliases` 被未来目标滥用导致展示面漂移] → 测试锁定：列表/报错/help 快照中不得出现别名；仅查找路径匹配。

## Migration Plan

纯增量：无存量数据迁移。新目标/新适配器不触碰既有 agent 的行为；`pi` 目标保持 `PI_CODING_AGENT_DIR` 无条件生效的旧语义（omp 的"仅默认 profile"规则不回流到 pi）。回滚 = revert 本 change。

## Open Questions

无。别名拼写（`oh-my-pi`）、冒烟深度、ADR 范围均已在 grilling 中定稿。
