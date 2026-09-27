# oh-my-pi（omp）agent 支持：YAML 成为第三种写回格式，配置面镜像 omp 而非沿用 pi

oh-my-pi（omp，can1357/oh-my-pi）是 Pi（mariozechner）的活跃 fork，但配置面已实质分化：MCP 内置（不再需要 pi-mcp-adapter），provider 配置从 `models.json` 迁到 **`models.yml`**，默认模型从 `settings.json` 的 `defaultProvider`/`defaultModel` 迁到 **`config.yml` 的 `modelRoles.default`**（`provider/model` 选择子）。senv 若复用现有 `pi` 目标写 `~/.pi/agent`，omp 根本不会读，等于静默失效。

## 决策

1. **omp 作为独立 Coding Agent 注册**，id `omp`（Name "Oh My Pi"），别名 `oh-my-pi` 仅在 `Find`/`LookupAgent` 查找时接受，列表/帮助/报错只显示规范 id。`mcp` 侧进 `agentcfg` 注册表（llm 侧进 `SupportedAgents`，TUI 两处清单均注册表驱动、零改动）。
2. **mcp 目标无 Prerequisite**：omp 内置 MCP，user 配置 `~/.omp/agent/mcp.json`、project `.omp/mcp.json`（cwd 相对，同 cursor 模式）。remote 条目写 `type: "http"|"sse"` 键 + `headers`；SSE 仍接受（omp 未废弃读取）但新配应以 http 为主。
3. **路径解析镜像 omp**（`OmpAgentDir`）：`OMP_PROFILE`（优先）/`PI_PROFILE` 激活时整体落到 `~/.omp/profiles/<name>/agent/`；profile 名非法时回退默认 profile（同 omp 的 `readProfileFromEnvSafe`）；`PI_CONFIG_DIR` 改配置根；`PI_CODING_AGENT_DIR` 仅默认 profile 时生效（profile 激活时 omp 忽略它，senv 同样忽略）；Linux XDG 重定向不支持（需用户先 `omp config migrate`，代码注释说明）。
4. **llm 适配器写 YAML**：`models.yml` 写 `providers.senv-<alias>` 条目（字段投影复用 pi 适配器：`baseUrl`/`apiKey`/`api`（`piAPIType`）/`models[]` 含 contextWindow/maxTokens/reasoning/input，provider 级 `compat.supportsDeveloperRole: false`——pi 踩过的 developer 角色 400 坑在 omp 原样存在）；`config.yml` 只写 `modelRoles.default`，不动 smol/slow/plan 等角色（未配置回退 default，与 [ADR-0029](./0029-claude-code-background-model.md) 范围一致）。新增 `applyYAMLMerge` 写回原语，与 `applyJSONMerge`/`applyTOMLMerge` 并列同一事务协议（备份 → temp+rename → 回滚）。
5. **凭据 CredentialInline**（解密明文写盘、0600），与 pi 完全一致。不利用 omp `apiKey` 支持环境变量名/`!cmd` 取密的能力——那要求用户每次启动 omp 前都先 `senv env export`，断了即 401，故障面比明文落盘更大；留作后续增强。
6. **别名机制通用化**：`Target`/`AgentAdapter` 加 `Aliases` 字段，查找函数遍历匹配，不特判 omp。

## Considered Options

- **复用 pi 目标、写 `~/.pi/agent`**：omp 的源码发现顺序不含 `.pi`（`docs/config-usage.md`「Important constraint」），静默失效。
- **写 `models.json` 依赖 omp 启动迁移**：迁移只在 `models.yml` 缺失时发生一次，且 omp 文档明示 JSON 路径仅 programmatic 支持；senv 写 JSON 会与用户既有的 models.yml 并存造成覆盖/竞争。
- **`apiKey` 写环境变量名间接引用**：见决策 5；且 env 名形态下 senv 无法校验凭据本机存在（codex 的 fail-closed 校验模式覆盖不到明文不落盘之外的新形态）。
- **为保注释引入 YAML 事件级 patcher**：yaml.v3 重写必丢注释；TOML 写回（codex config.toml）已有同等先例，成本收益不成比例。

## Consequences

- `gopkg.in/yaml.v3` 从 indirect 转直接依赖；写回原语集从 JSON/TOML 扩为 JSON/TOML/YAML 三原语。
- `config.yml`/`models.yml` 被 senv 重写时用户注释丢失（同 TOML 先例）；install/switch 输出不加警告。
- spec `llm-provider-switch` 与 `mcp-server-export` 的受支持 agent 枚举随本 change 同步。
- 上游漂移风险：omp 演进快（本决策锚定 v18.3.3 实测 + docs/models.md、mcp-config.md、dirs.ts），其 YAML schema、profile 规则变化时 `OmpAgentDir` 与适配器需跟改；skill 文档记录验证锚点。
- 本机冒烟用临时 HOME（`HOME=/tmp/...`）隔离，不触碰用户真实 `~/.omp`。

## Status

accepted（2026-09-27 随 change `add-oh-my-pi-agent` 实现落地；本机 omp v18.3.3 临时 HOME 冒烟通过）
