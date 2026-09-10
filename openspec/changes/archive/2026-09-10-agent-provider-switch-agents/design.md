## Context

store 子 change 已交付 `ProviderManager`（add/list/show/remove）与加密档案 `LLMProviderEntry`（alias、base_url、credential_ref、models、default_model）。catalog 子 change 交付模型目录缓存。既有 `cmd/mcp_agents.go` 维护了 agent 配置路径注册表（MCP 安装用途），本 change 新建 provider 切换专用注册表，不复用其 mcpServers 写回逻辑。

已核实的配置事实：

| agent | 配置文件 | 格式 | provider 相关结构 |
|-------|---------|------|------------------|
| claude-code | `~/.claude/settings.json` | JSON | 顶层 `model` + `env.{ANTHROPIC_BASE_URL, ANTHROPIC_AUTH_TOKEN}`（官方 env-vars/settings 文档确认，env 值每次会话自动生效） |
| codex | `~/.codex/config.toml` | TOML | 顶层 `model`/`model_provider` + `[model_providers.<id>]`（name/base_url/env_key/wire_api/requires_openai_auth）；key 只走环境变量（本机真实配置 + 官方 config-reference 双重确认） |
| zcode | （降级 unsupported） | — | `~/.zcode/v2/config.json` 官方 FAQ 确认存模型供应商配置（API Key/Base URL/模型），但 schema 由 GUI 写入、无公开文档、本机无真实样本；按 D-E 降级，status 明示不支持 |
| kimi | `~/.kimi-code/config.toml` | TOML | `default_model` + `[providers."<name>"]`（type/base_url/api_key 明文，CLI 只从配置读凭证）+ `[models."<alias>"]`（provider/model/max_context_size 必填/display_name）（Kimi Code CLI 官方配置文档确认；旧 `~/.kimi/mcp.json` 是 MCP 配置，与 provider 无关） |
| pi | `~/.pi/agent/models.json` + `~/.pi/agent/settings.json` | JSON | `providers.<id>` = `{baseUrl, api, apiKey, models:[{id,...}]}`（官方 models.md）；默认指向写 settings.json 的 `defaultProvider`/`defaultModel`（官方 settings.md） |
| opencode | `~/.config/opencode/opencode.json` | JSON | `provider.<id>` = `{npm:"@ai-sdk/openai-compatible", name, options:{baseURL, apiKey}, models:{<modelId>:{name}}}` + 顶层 `model = "<id>/<model>"`（官方 config/providers 文档） |
| cursor | — | — | 明确不支持（D2 降级） |

## Goals / Non-goals

**Goals:**

- 一条命令完成「指向哪个 provider 的哪个模型」，覆盖 6 个 agent
- 配置写回可预测：merge 保留无关键、原子替换、留备份、失败可回滚
- 指针与配置的一致性由 senv 保证（指针永远描述最近一次成功切换）

**Non-goals:**

- 不解析 agent 配置反推状态；agent 手工改配置后 senv 不感知（D7 取舍）
- 不为 cursor 寻找兼容写法
- 不做凭据代理/注入进程环境

## Decisions

### D-A 切换注册表与适配器独立于 MCP 安装表

`internal/llm` 内新建：

```go
type AgentAdapter struct {
    ID, Name      string
    ConfigPath    func(home string) string
    Apply         func(req SwitchRequest) error   // merge + 原子写 + 备份
    Credential    CredentialMode                  // InlineFile / EnvVarName
}
```

`SwitchRequest` 携带 configPath、providerAlias、baseURL、model、credential（解密后明文或 env key 名）。cursor 不入注册表，由独立 unsupported 列表供 status 展示。

### D-B 凭据两种归宿

- `InlineFile`（claude-code、zcode、kimi、pi、opencode）：解密明文写入配置文件，文件权限收敛 0600。这是 grill 决策 4 接受的妥协（agent 无法自行访问 vault），ADR 归档时晋升。
- `EnvVarName`（codex）：仅写 `env_key` 名称，输出指引用户通过 senv env 能力暴露 key。密钥明文绝不落 config.toml。

### D-C 原子写协议

1. 读原文件（不存在则视为空配置）；序列化 merge 结果到同目录临时文件（0600）
2. 复制原文件到 `<config>.senv-bak`
3. rename 临时文件 → 配置文件
4. SwitchManager 更新指针；失败则从备份恢复配置并删除备份副本成功后保留

JSON merge 用 `map[string]any` + 递归合并；TOML 用已有的 TOML 库（与 mcp_agents.go 同源）解析后改字段再序列化。

### D-D 指针存储

`~/.config/senv/agent-pointers.json`：`{"agents":{"<id>":{"provider":"...","model":"...","switched_at":"..."}}}`，0600。读写走 `internal/llm/pointer.go`，不经过 storage.Manager（明确非 vault 资产）。

### D-E 勘察降级路径

实施首个任务核实 claude-code/zcode/kimi 真实格式；无法核实的 agent 从注册表降级为 unsupported（同 cursor 处理），status 明示，不猜测格式写坏用户配置。

## 数据流

```
senv ai switch <agent> <provider> [--model]
  → vault: LoadLLMProvider(provider)            # 档案
  → vault: text/env manager Get(credential_ref) # 解密凭据
  → adapter.Apply(baseURL, model, credential)   # merge + 原子写
  → pointer.Save(agent, provider, model)        # 失败则回滚配置
```

## 错误处理策略

- 校验类错误（agent/provider/model）在写任何文件前失败
- 写回错误保持原配置不变；指针错误回滚配置
- 凭据解密失败视为校验类错误，不进入写回阶段

## Risks / Trade-offs

- agent 配置由 senv 与用户双写，可能漂移 → D7 接受；status 以指针为准
- 明文 key 落 agent 配置 → 决策 4 妥协，权限 0600 收敛，ADR 晋升
- 勘察失败导致覆盖面缩窄 → 降级为 unsupported，不猜格式

## Migration Plan

纯新增命令与文件；不迁移既有数据。首次 switch 生成指针文件。

## Open Questions

（无——待勘察项已由任务 1 兜底为降级路径）
