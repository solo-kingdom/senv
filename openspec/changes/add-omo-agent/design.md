## Context

senv 的 coding agent 支持分两个注册表：`internal/agentcfg`（`Target`，mcp install/export）与 `internal/llm`（`AgentAdapter`，`ai switch`）。两个注册表都被 CLI 与 TUI 共用，新增 agent 即注册表加条目，展示面自动跟进。

OmO（`code-yeongyu/oh-my-openagent`，本机 v5.1.47 实测）是 senpi/pi 的改名 fork，品牌层只换了名字：

| 面 | OmO | pi |
|---|---|---|
| agent 目录 | `~/.omo/agent` | `~/.pi/agent` |
| LLM provider | `models.json` 的 `providers.<id>` | 同左 |
| 默认模型 | `settings.json` 的 `defaultProvider`/`defaultModel` + `enabledModels` | 同左 |
| MCP | `mcp.json`（内置客户端） | `mcp.json`（需 pi-mcp-adapter 扩展） |
| 环境变量前缀 | `OMO`（`OMO_CODING_AGENT_DIR`） | `PI_` |

事实证据见任务台账 `context/current.md`，取自二进制内置文档与符号表，非社区推测。

## Goals / Non-Goals

**Goals:**

- `agentcfg` 新增 `omo` 目标：install/export 写 `~/.omo/agent/mcp.json`（user）与 `.omo/mcp.json`（project），remote 只接受 stdio/http。
- `llm` 新增 `omoAdapter`：写 `models.json` provider 条目与 `settings.json` 默认模型，`enabledModels` 置顶。
- 抽出 pi/omo 共享的写回函数，避免两份近似代码漂移。

**Non-Goals:**

- 不跟随 OmO 从 cwd 向上找 `.omo/agent` 的项目级重定向。
- 不支持 sse 传输导出到 OmO。
- 不加 `skill apply --target omo`。
- 不改其它 agent 行为，不接管 `omo.jsonc`、subagent 分类模型等 fork 特有概念。

## Decisions

1. **`OmoAgentDir` 只读三个环境变量，不跟随项目级目录搜索。**
   OmO 的 `resolveAgentDir(cwd, homeDir, envDir)` 会在无环境变量时从 cwd 向上找 `.omo/agent`。senv 不镜像该行为，因为：`AgentAdapter.ConfigPath(home)` 签名只有 home，`ai switch` 没有 project 语义；若 MCP 侧跟随 cwd 而 LLM 侧不跟随，同一 agent 会有两套「配置在哪」的答案；且 cwd 依赖会让同一命令在不同目录写不同文件，与 user scope 的全局语义冲突。pi 的 `PiAgentDir` 面对同样的上游行为也只看环境变量，属既有约定。代价与绕法（用 `--scope project`）写进 ADR 与 skill 文档。

2. **Remote 只声明 `HTTP: true, SSE: false`。**
   OmO 的 `ServerSchema` 把 `type` 限定为 `Literal("stdio")|Literal("http")`，且 `additionalProperties:false`；SSE 是 http 连接内部的降级路径，不是可写的类型值。把 sse 档案写成 `type:"http"` 属静默改写，按 codex 先例改为 plan-error。`Reason` 里写明原因，用户在计划里看到而不是得到一份被悄悄改过的配置。

3. **`llm` 侧抽共享函数 `applyPiFamily`，不复制 pi 的 `Apply`。**
   OmO 与 pi 的 provider 条目、settings 键名、`enabledModels` 重排规则逐字相同，只差 `ConfigPath` 的目录。复制会让两处各自演化（例如将来 pi 改 enabledModels 语义时 OmO 不同步）。抽取时严格只做「搬函数不改行为」，由既有 pi 单测兜底；若抽取需要改 pi 语义，退回各写一份并在 ADR 记录。

4. **凭据用 `CredentialEnvRef` 返回 `"$" + name`，与 pi 一致。**
   OmO 的 `apiKey` 支持 `$VAR`/`${VAR}` 插值（`docs/models.md` 值解析小节），因此 ADR-0031 的「默认只写引用」直接适用，不需要 `CredentialInline` 特例。

5. **`enabledModels` 置顶是必需的，不是可选优化。**
   OmO 启动选择顺序是 CLI 显式 > `scopedModels[0]`（由 `enabledModels` 解析）> `defaultProvider`/`defaultModel`，且非续接会话时首个 scope 压过 default。只写 `defaultProvider`/`defaultModel` 而不置顶 `enabledModels`，在用户已有 `enabledModels` 时切换不生效。直接复用 `updatePiEnabledModels`。

6. **skill 面零改动。**
   OmO 的 `loadSkills2` 已把 `~/.agents/skills` 与项目 `.agents/skills` 纳入发现根，正是 `senv skill apply` 的两个默认根之一。加 `--target omo` 只会造成同一 skill 落两份。结论写进 ADR 与 senv-cli skill，避免用户误以为不支持。

7. **别名为 `oh-my-openagent`。**
   与 omp ← `oh-my-pi` 同规则：仅查找接受，列表/报错/help 只显示 `omo`。

## Risks / Trade-offs

- **共享写回函数导致 pi 回归** → 只搬不改；pi 的既有单测（`modelset_projection_test.go`、`modelmeta_test.go` 等）验证写回形状；抽取后先跑全量测试再继续。
- **OmO 上游演进快，品牌目录或 schema 可能漂移** → 所有镜像逻辑附二进制/文档锚点注释（v5.1.47）；skill 文档记录验证锚点；冒烟用本机真实 omo。
- **项目级 `.omo/agent` 遮住 senv 的 user scope 写入** → 属已知偏差，写入 ADR 与 skill；本机当前不存在该目录。
- **sse 档案无法导出到 OmO** → 明确 plan-error 并给原因，用户可改用 http 档案；不静默改写。

## Migration Plan

纯增量：无存量数据迁移。首次 `ai switch omo` 或 `mcp export --agent omo` 才创建 `~/.omo/agent` 下的配置。回滚 = revert 本 change；senv 写过的条目按既有 ledger 语义（`senv mcp unexport`）可撤回，`settings.json` 的默认键与 `models.json` 的 `senv-<alias>` provider 由下次切换自然收敛。

## Open Questions

无。路径解析取舍、sse 处理、skill 面结论均已在 taskrail intent 中与用户确认。
