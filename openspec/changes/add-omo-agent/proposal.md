# OmO（oh-my-openagent）agent 支持

## Why

OmO（`code-yeongyu/oh-my-openagent`，本机 v5.1.47）是 senpi/pi 的改名 fork，配置形状与 pi 相同，只是品牌目录为 `~/.omo/agent`、环境变量前缀为 `OMO`。senv 现在把 pi 的配置写到 `~/.pi/agent`，OmO 不读那里，因此「senv 是唯一事实源」在 OmO 上断掉：LLM provider 与 MCP 都得手工配。用户已在用 OmO，需要 senv 两侧都认识它。

## What Changes

- `internal/agentcfg` 新增 `omo` 目标（别名 `oh-my-openagent`）：内置 MCP 无前置依赖；user `~/.omo/agent/mcp.json`、project `.omo/mcp.json`；remote 写 `type` 键 + headers，但只接受 stdio/http（OmO 的 mcp.json schema 无 `sse` 类型）。
- 新增 `OmoAgentDir(home)`：只镜像 `OMO_CODING_AGENT_DIR` → `SENPI_CODING_AGENT_DIR` → `PI_CODING_AGENT_DIR` → `~/.omo/agent`，不跟随 OmO 的项目级目录搜索（理由见 design）。
- `internal/llm` 新增 `omoAdapter`：写 `models.json` 的 `providers.<id>` 与 `settings.json` 的 `defaultProvider`/`defaultModel`，`enabledModels` 非空时把 `senv-<alias>/<默认模型>` 置顶；与 pi 共用抽取出的写回函数。
- 凭据 `CredentialEnvRef` 用 `$NAME`（OmO 的 `apiKey` 支持 `$VAR` 插值，与 pi 同）。
- skill 面不改：OmO 已扫描 `~/.agents/skills`，即 `senv skill apply` 的现有默认根；结论写入 ADR 与 senv-cli skill。
- 文档：ADR-0032、`.agents/skills/senv-cli/SKILL.md`、`README.md`。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider-switch`：受支持 agent 注册表加入 omo；新增 omo 的 JSON 写回要求（`models.json` provider 条目 + `settings.json` 两个默认键 + `enabledModels` 置顶）、配置路径解析、兼容字段投影；别名枚举同步。
- `mcp-server-export`：受支持导出/安装目标加入 omo（内置 MCP、user/project 双路径、remote 仅 http、sse 必须 plan-error）。

## Non-goals

- 不跟随 OmO 的项目级 `.omo/agent` 目录搜索（pi 先例同样不跟随）。
- 不支持 sse 传输导出到 OmO（schema 无该类型，静默改写会生成 OmO 可能拒载的配置）。
- 不为 OmO 增加 `senv skill apply --target omo`（现有 `.agents/skills` 根已覆盖）。
- 不改 pi / claude-code / codex / kimi / opencode / omp 任何既有行为。
- 不接管 OmO 的 `omo.jsonc`、subagent 分类模型、`modelRoles` 之类 pi fork 特有概念（pi 侧也没有对应能力）。

## Impact

- 代码：`internal/agentcfg/agents.go`（新目标 + `OmoAgentDir`）、`internal/llm/switch.go`（`omoAdapter` + pi/omo 共享写回函数）。
- 测试：`internal/agentcfg/config_test.go`、`internal/llm/*_test.go`、`cmd/mcp_export_test.go`。
- 文档：`docs/adr/0032-omo-agent-json-writeback.md`、`.agents/skills/senv-cli/SKILL.md`、`README.md`。
- 安全：写盘面与 pi 相同（ADR-0002 明文取舍、0600 收敛）；凭据默认走环境变量引用（ADR-0031），不新增暴露面。

## 安全性分析

OmO 适配器完全复用 pi 路径：解密引用 → 按 `CredentialEnvRef` 写 `$NAME` 或回落明文 → 文件权限收敛 0600，与 ADR-0002 / ADR-0031 的既定取舍一致。新增的唯一写盘目标是 `~/.omo/agent/{models.json,settings.json,mcp.json}`，由 `agentcfg.WriteWithBackup` 备份后原子替换，不触碰 OmO 的其它文件（`auth.json`、`sessions`、`extensions` 等）。别名只影响查找，不改变写盘目标。
