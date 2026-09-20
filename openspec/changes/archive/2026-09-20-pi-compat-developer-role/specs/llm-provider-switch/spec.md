## ADDED Requirements

### Requirement: pi 兼容字段投影

`senv ai switch` 写 pi 的 provider 定义时 SHALL 一并写 provider 级 `compat`，其中 `supportsDeveloperRole` MUST 为 `false`。理由：pi 的 `openai-completions` 与 `openai-responses` 路径都以 `model.reasoning && compat.supportsDeveloperRole` 决定 system prompt 用 `developer` 还是 `system` 角色，而 `supportsDeveloperRole` 的缺省推断依赖一份硬编码的 base URL / provider 特征名单；senv 写入的自建网关地址不在名单内，会被判成标准 OpenAI，使被投影为 `reasoning: true` 的模型发出上游不接受的 `developer` 角色（Moonshot/Kimi 等以 400 `role 'developer' is not allowed` 拒绝）。该声明是 provider 级兼容开关，MUST NOT 视为模型元数据推断，因此不受「某项元数据缺失即省略该字段」规则约束；MUST 覆盖该 provider 下的全部模型（含非推理模型与重跑后新增的模型），MUST NOT 按模型分支或在模型集变化时丢失。`compat` 中 MUST NOT 出现 `supportsReasoningEffort: false`——真 OpenAI 端点需要它透传 `reasoning_effort`，一刀切会剥夺推理档位。

#### Scenario: 受影响的推理模型不再发 developer 角色

- **WHEN** 用户切换 pi 至某 provider，其档案为模型 m1 保存了推理档位（投影为 `reasoning: true`），接入地址为自建网关
- **THEN** `providers.<id>` 含 `compat.supportsDeveloperRole: false`，m1 条目仍含 `reasoning: true`，pi 发首条消息使用 `system` 角色，不再出现上游 400 `role 'developer' is not allowed`

#### Scenario: 覆盖整个 provider 而非单个模型

- **WHEN** 某 provider 的模型集同时含推理模型与非推理模型，或用户以新模型集重跑切换
- **THEN** `compat.supportsDeveloperRole: false` 始终写在 provider 级并对其下全部模型生效，模型集增删不改变该字段

#### Scenario: 声明 openai-responses 时同样投影

- **WHEN** 档案 `api_shape` 为 `openai-responses`，用户切换 pi
- **THEN** provider `api` 写 `openai-responses`，`compat.supportsDeveloperRole: false` 一并写出（两条线协议路径共用该判定）

#### Scenario: 不连带关闭推理档位透传

- **WHEN** 切换 pi 至任一 provider
- **THEN** provider `compat` 中 MUST NOT 出现 `supportsReasoningEffort: false`；指向真 OpenAI 端点时 `reasoning_effort` 照常透传
