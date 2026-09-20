## Context

动机与症状见 `proposal.md`。设计相关的现状约束：

- pi 的 provider 对象由 senv **整体拥有**：`piAdapter().Apply` 里 `providers[senv-<alias>] = map[string]any{...}` 每次切换整体替换，用户手工加的字段必然丢失。这就是本变更必须落到代码、而不能只靠文档让用户手改的原因。
- 该字段落在 pi 的 `~/.pi/agent/models.json`，与 `settings.json`（`defaultProvider`/`defaultModel`/`enabledModels`）同属一次事务。
- pi 支持 provider 级 `compat`，并与模型级 `compat` 合并（模型级覆盖 provider 级）；provider 级声明天然覆盖该 provider 下所有模型。
- 现有投影逻辑只在「元数据已知」时写字段，且禁止写 `false`/`0`/空数组（避免把 pi 内置默认打坏）。`compat` 是一个显式兼容声明，不属于该规则。

## Goals / Non-Goals

**Goals:**

- 切换 pi 后，被投影为 `reasoning: true` 的模型在自建网关 provider 上能正常发首条消息（不再出现 `role 'developer' is not allowed`）。
- 该修复对模型集变化、线协议变化（chat/responses）都稳定，不需要用户维护任何本机手工补丁。

**Non-Goals:**

- 不改 `reasoning: true` 投影本身（推理能力元数据是对的，问题只在角色选择）。
- 不改 `supportsReasoningEffort`（见 Decisions 3）。
- 不引入 base URL 特征推断（见 Decisions 2）。
- 不改其他 agent 适配器，不改档案 schema、CLI、TUI。

## Decisions

### 1. 写 provider 级而非模型级 `compat`

**选择**：`providers.<id>.compat = {"supportsDeveloperRole": false}`，一个 provider 写一次。

**理由**：该开关与「哪个模型」无关，只与「这个接入端点能不能吃 `developer` 角色」有关，而 senv 的一个 provider 定义就是一个端点。provider 级同时天然覆盖后续重跑新增的模型，不需要在模型集差集清理逻辑里额外照顾。

**备选**：模型级 `compat`（每个 `models[]` 条目各写一份）——能表达「同一端点下不同模型不同待遇」，但本场景不存在这种需求，且会让模型集差集清理、`使用档案内模型元数据` 等既有分支都多一个字段要维护。

### 2. 一律写 `false`，而不是条件写

**选择**：所有 pi provider 都写 `supportsDeveloperRole: false`。

**理由**：判定「哪些端点支持 `developer`」pi 自己都只能靠硬编码 URL 特征名单，senv 若复制一份必然随 pi 发版漂移（ADR 级的长期成本）。而 `system` 角色是所有 OpenAI 兼容端点的公共子集——真 OpenAI 端点同样接受 `system`，`developer` 只是新式等价角色。因此无条件写 `false` 是「最小风险面」而非「降级」。

**备选**：

- 只在 `modelSupportsReasoning(meta)` 为真时写 —— 减少影响面，但要求 provider 级 `compat` 退化为模型级写法，且模型级元数据变化会牵动 compat，耦合更差。
- senv 抄 pi 的 `isNonStandard` 名单 —— 名单漂移风险最高，pi 加一个厂商就要跟一次。
- 给档案加「非标准 OpenAI 兼容」声明字段由用户标注 —— 语义最干净，但每个自建网关档案都要手工标一次，漏标即复现本 bug；且要动 vault schema 与 CLI/TUI 三处，成本远超收益。

### 3. 不连带写 `supportsReasoningEffort: false`

**选择**：只关 `supportsDeveloperRole`。

**理由**：`supportsReasoningEffort` 控制 `reasoning_effort` 是否透传。把它也关掉会让指向真 OpenAI 的档案失去推理档位控制，属于真实功能降级。它在本机上可能与同一份误判同样命中（自建网关的 Kimi 渠道若不接受 `reasoning_effort` 会在开启 thinking 时报另一个错），但那是一个**独立、可观测、症状不同**的问题，应按实际报错与上游能力单独决策，不能借这次修复顺手关掉。

### 4. 数据流

```
vault 档案 (api-itn)
   │  models + per-model 元数据(reasoning 档位/context/模态)
   ▼
senv ai switch pi api-itn
   │  ResolveModelMetadata → entry{contextWindow,maxTokens,reasoning,input}
   ▼
piAdapter().Apply
   │  providers["senv-api-itn"] = {            ← 整体替换（senv 拥有）
   │      baseUrl, api, apiKey,
   │      compat: {supportsDeveloperRole:false},   ← 本变更新增
   │      models: [...]
   │  }
   ▼
~/.pi/agent/models.json   ──┐
~/.pi/agent/settings.json ──┴─ 同一事务，第二份失败回滚第一份
   ▼
pi 侧请求：role = model.reasoning && compat.supportsDeveloperRole ? "developer" : "system"
   ▼                                        ↑ 现恒为 false
Moonshot/Kimi 上游：只接受 system → 不再 400
```

### 5. 错误处理策略

本变更不引入新的失败路径：只是在既有 map 构造里多一个常量键，不产生错误、不影响序列化顺序（单键 map，Go 的 `encoding/json` 对 map 键排序输出）。写回失败仍由 `applyJSONMerge` + `SwitchManager` 的既有事务/回滚语义处理，pi 的多文件回滚策略不变。

## Risks / Trade-offs

- **[真 OpenAI 档案从 `developer` 退回 `system`]** → OpenAI 兼容两种角色且语义等价，功能无损失；若将来确有端点只认 `developer`，再按档案加声明式开关（本设计的备选 3）。
- **[依赖 pi 的非公开契约]** → `supportsDeveloperRole` 是 pi 官方文档化的字段（`docs/models.md` 的 compat 表格与 `OpenAI Compatibility` 一节），不是内部实现细节。
- **[用户此前已手工打过补丁，误以为补丁会保留]** → senv 本来就整体拥有 `providers.senv-<alias>`，本变更让手工补丁不再需要；在代码注释里写明该 provider 对象由 senv 拥有。
- **[开启 thinking 后仍可能撞 `reasoning_effort` 不被上游支持]** → 明确记为 Non-Goal 与本变更外；待实际报错按端点单独处理（见 Decisions 3）。
- **[投影字段被未来「缺失即省略」重构误删]** → 加注释 + 单测断言，spec 中亦有 `MUST` 约束。

## Migration Plan

无数据迁移。用户在受影响机器上重跑一次 `senv ai switch pi <provider>` 即写回 `compat`；手工补丁的历史不再需要。

回滚策略：`git revert` 即可，写回是幂等的，pi 侧在字段缺失时按推断恢复旧行为。

## Open Questions

- 自建网关的 Kimi 渠道是否接受 `reasoning_effort`（影响 `think:low/high/max`）——需要在真实端点开启 thinking 实测，与本次修复正交，不影响 specs 与任务拆分。
