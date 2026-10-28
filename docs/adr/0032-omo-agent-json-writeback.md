# OmO（oh-my-openagent）agent 支持：复用 pi 的配置形状，路径与能力按 OmO 实测收窄

OmO（`code-yeongyu/oh-my-openagent`）是 senpi/pi 的改名 fork。与 omp（[ADR-0030](./0030-oh-my-pi-agent-yaml-writeback.md)）不同，它**没有分化配置面**：品牌层只换了名字（`OmO` / `omo` / `.omo` / `OMO_`），provider 仍是 `models.json` 的 `providers.<id>`，默认模型仍是 `settings.json` 的 `defaultProvider`/`defaultModel` 加 `enabledModels`，MCP 是内置客户端。所以 senv 不需要新写回原语，只需要新路径与两处能力收窄。

## 决策

1. **OmO 作为独立 Coding Agent 注册**，id `omo`，Name `OmO`，别名 `oh-my-openagent` 仅查找接受，展示面只显示 `omo`（沿用 [ADR-0030](./0030-oh-my-pi-agent-yaml-writeback.md) 决策 1/6 的别名规则）。
2. **路径只读三个环境变量**（`OmoAgentDir`）：`OMO_CODING_AGENT_DIR` → `SENPI_CODING_AGENT_DIR` → `PI_CODING_AGENT_DIR` → `$HOME/.omo/agent`。值原样使用，不展开 `~`（OmO 自身用 Node `path.resolve`，`~` 是字面量；senv 的 pi 规则同样不展开）。
3. **不镜像 OmO 的项目级目录搜索**。OmO 的 `resolveAgentDir(cwd, homeDir, envDir)` 在无环境变量时会从 cwd 向上找 `.omo/agent`。senv 不跟随，因为 `AgentAdapter.ConfigPath(home)` 只有 home、`ai switch` 没有 project 语义，而 `mcp export` 的 user scope 是全局语义；两条路径若一个跟随 cwd 一个不跟随，同一个 agent 会出现两套「配置在哪」的答案，同一命令在不同目录还会写出不同文件。pi 面对同样的上游行为也只读环境变量，属既有约定。需要项目级 MCP 配置时用 `--scope project`（写 `.omo/mcp.json`）；项目级 `.omo/agent` 会遮住 senv 的 user scope 写入，这一点写进 skill 文档。
4. **MCP remote 只接受 stdio 与 http，sse plan-error**。OmO 的 `mcp.json` 用 `ServerSchema` 校验，`type` 的取值是 `Literal("stdio") | Literal("http")`，且 `additionalProperties: false`；SSE 是 http 连接内部的降级路径（`docs/mcp.md`：「Streamable HTTP with SSE fallback」），不是可写的类型值。把 sse 档案写成 `type: "http"` 是静默改写，沿用 codex 的先例改为计划阶段报错。与 omp 不同——omp 的 schema 接受 `"sse"`。
5. **llm 侧抽共享写回体 `applyPiFamily`**。OmO 的 provider 条目、`settings.json` 键名、`enabledModels` 重排规则与 pi 逐字相同，只差目录。复制会让两处各自漂移。抽取只搬不改，由 pi 的既有单测兜底；若需改 pi 语义则退回各写一份。
6. **凭据走 `CredentialEnvRef` 返回 `"$" + name`**。OmO 的 `apiKey` 支持 `$VAR`/`${VAR}` 插值（`docs/models.md`），因此 [ADR-0031](./0031-credential-env-mode.md) 的「默认只写引用」直接适用，不需要 inline 特例。不支持插值时回落明文，文件收敛 0600（[ADR-0002](./0002-llm-provider-switch.md) 的既定取舍）。
7. **`enabledModels` 置顶是必需项**。OmO 的启动模型选择顺序是 CLI 显式 > `enabledModels` 解析出的 `scopedModels[0]` > `defaultProvider`/`defaultModel`，且非续接会话时首个 scope 压过 default。只写两个 default 键而用户已有 `enabledModels` 时切换不生效，因此复用 `updatePiEnabledModels`。
8. **skill 面零改动**。OmO 的 skill 发现根包含 `~/.agents/skills` 与项目 `.agents/skills`，正是 `senv skill apply` 的默认根。加 `--target omo` 只会让同一 skill 落两份。结论写进 `.agents/skills/senv-cli/SKILL.md`，避免用户误以为不支持。

## Considered Options

- **复用 pi 目标、写 `~/.pi/agent`**：OmO 只读 `.omo`（本机 `omo --help` 实测输出「OmO reads its config from `/home/<user>/.omo/agent` now; edits to `/home/<user>/.pi/agent` no longer reach OmO」），静默失效。
- **完全镜像 OmO 的目录解析（含 cwd 搜索）**：见决策 3；会破坏 user scope 的全局语义，且 LLM 侧无法实现同等语义。
- **把 sse 档案改写成 http 导出**：见决策 4；生成的是用户没要求的配置，且 OmO 文档把 SSE 说成 http 内部 fallback，语义不等价。
- **给 skill apply 加 `omo` 目标根**：见决策 8；`.agents/skills` 已覆盖，多一个根等于多一份冗余落盘与维护面。
- **为 OmO 接管 `omo.jsonc` 的 subagent 分类模型**：pi 侧也没有对应能力，属 fork 特有概念，超出「导出 provider / MCP」的任务边界。

## Consequences

- 写回原语不新增（仍 JSON/TOML/YAML 三原语），新增的是一个 JSON 目标与一个 JSON 适配器。
- pi 的 `Apply` 被抽出 `applyPiFamily`，是全仓唯一被本决策改动的既有路径；回归由 `internal/llm` 的既有 pi 测试覆盖。
- spec `llm-provider-switch` 与 `mcp-server-export` 的受支持 agent 枚举与能力声明随本 change 同步（新增「omo 配置路径解析」「omo 兼容字段投影」「omo 导出与安装目标」「omo 拒绝 sse 传输」）。
- 上游漂移风险：本决策锚定 OmO v5.1.47（二进制内置文档 `docs/{mcp,models,settings,skills}.md` 与 `ServerSchema`/`resolveAgentDir` 符号）。品牌目录、schema 取值或环境变量优先级变化时 `OmoAgentDir` 与目标声明需跟改；skill 文档记录验证锚点。
- 冒烟用隔离目录 `OMO_CODING_AGENT_DIR=<tmp>`，不触碰用户真实 `~/.omo/agent`。

## Status

accepted（2026-10-28 随 change `add-omo-agent` 实现落地；本机 OmO v5.1.47 隔离目录冒烟通过）
