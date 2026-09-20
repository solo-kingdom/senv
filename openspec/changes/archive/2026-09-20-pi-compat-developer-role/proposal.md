## Why

`senv ai switch pi <自建网关 provider>` 后，凡是被投影为 `reasoning: true` 的模型在 pi 里一发消息就 400：`role 'developer' is not allowed`（Moonshot/Kimi 上游报错，new-api 只透传）。

根因：pi 的 `openai-completions` 路径这样选 system prompt 的角色（`dist/bundle/chunks/openai-completions-*.js`）：

```js
let role = model.reasoning && compat.supportsDeveloperRole ? "developer" : "system";
```

而 `compat.supportsDeveloperRole` 的默认推断是一份硬编码 URL / provider 特征名单（`api.moonshot.`、`deepseek.com`、`api.z.ai`、`together`、`cerebras`、`nvidia`、`cloudflare`、`ant-ling`、`opencode.ai`）——senv 写入的自建地址（如 `http://token-api-itn.wii.pub/v1`）一个都不匹配，于是被判成「标准 OpenAI」，`supportsDeveloperRole: true`。senv 侧又只投影 `reasoning: true` 而不写 `compat`，两个条件一起命中 `developer`。非 reasoning 模型（MiniMax/GLM/DeepSeek 在档案里没有档位元数据）走 `system` 所以正常。

pi 官方 `docs/models.md` 就是给这种情况留的口子：`compat.supportsDeveloperRole: false`。senv 没写，用户只能手工改 `~/.pi/agent/models.json`，且下次 `switch` 即被覆盖。

## What Changes

- pi 适配器写 provider 定义时一并写 provider 级 `compat: {"supportsDeveloperRole": false}`
- 该声明覆盖 provider 下全部模型（含后续重跑新增的模型），不按模型分支
- 加注释说明这是一个兼容开关而非模型元数据投影，避免后续被「缺失即省略」规则误删

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider-switch`: 新增要求「pi 兼容字段投影」——pi 的 provider 定义 MUST 带 provider 级 `compat.supportsDeveloperRole: false`，且 MUST NOT 连带关掉 `supportsReasoningEffort`

## Non-goals

- 改动 `chat`/`responses` 线协议选择或推理档位投影（`reasoning: true` 保持）
- 一并写 `supportsReasoningEffort: false`：真 OpenAI 端点需要它来透传 `reasoning_effort`，一刀切会降级
- 在 senv 里复制一份 pi 的 base URL 特征名单做条件推断
- 为其他 agent（claude-code/kimi/opencode）引入同类开关
- 改动档案 schema、CLI flag 或 TUI 交互（用户不可见的行为修正由 `switch` 自动生效）

## Impact

- `internal/llm/switch.go` — `piAdapter().Apply` 的 provider 对象构造（现为 `baseUrl`/`api`/`apiKey`/`models`）
- `internal/llm/switch_test.go` — pi 投影断言
- `openspec/specs/llm-provider-switch/spec.md` — 经本变更的 delta 晋升

不改：vault 存储格式、指针文件、凭据处理路径、`kimi` 适配器（Kimi Code CLI 是另一个 agent，配置面与 pi 无关）。

**安全性分析**：仅新增一个布尔兼容字段，不触及凭据、地址门禁或文件权限；写回仍走既有 `applyJSONMerge` 事务与 0600 收敛。
