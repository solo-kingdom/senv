## MODIFIED Requirements

### Requirement: 编辑 LLM Provider 档案
`senv ai provider edit <alias>` SHALL 支持修改 base_url、模型集、模型 context window、模型输出上限、模型推理档位、默认推理档、输入模态、default_model、目录来源、凭据引用与 `api_shape`；alias 是主键 MUST NOT 被修改。凭据轮换语义 SHALL 与 `add` 一致：提供新自有凭据 SHALL 覆盖旧自有凭据；改为外部引用 SHALL 删除原自有凭据；未提供凭据来源 SHALL 保留既有凭据与引用。改动模型集、目录来源、模型 context window、推理档位或默认推理档时，必填元数据校验 SHALL 与 `add` 一致；仅修改其他字段（含只补输出上限/输入模态）时 MUST NOT 因旧档案缺少模型元数据而失败。校验（HTTPS 默认、拒绝 userinfo、模型集非空、`--default-model` 属于模型集、`--api-shape` 取值合法、默认推理档属于档位列表）SHALL 与 `add` 一致；任一步失败 MUST NOT 留下部分更新。编辑入口（CLI 与 TUI 同一方法）显式清空某个元数据字段时（默认推理档、模型输出上限、输入模态、模型 context window、模型推理档位），对应元数据 MUST 从档案中移除，MUST NOT 因档案既有元数据回填而静默保留旧值；清空后无法满足必填元数据校验的，SHALL 与 `add` 一致报错且不留下部分更新。TUI SHALL 通过同一方法提供等价编辑，别名在编辑态只读。

#### Scenario: 为旧档案补 context window
- **WHEN** 旧档案只有模型名，用户执行 `edit main --model-context m1=1000000`
- **THEN** 模型集保持不变，档案保存 m1 的 context window，后续切换无需 models.dev 缓存

#### Scenario: 为旧档案补默认推理档
- **WHEN** 旧档案 m1 已有推理档位 `low;high` 但无默认推理档，用户执行 `edit main --model-default-reasoning m1=high`
- **THEN** 模型集与档位保持不变，档案保存 m1 的默认推理档为 `high`

#### Scenario: 清空默认推理档生效
- **WHEN** 档案 m1 已保存默认推理档 `high`，用户在编辑入口清空默认推理档字段（表单置空）并提交
- **THEN** m1 的默认推理档从档案移除，推理档位与模型集保持不变，详情不再展示默认推理档

#### Scenario: 清空输出上限生效
- **WHEN** 档案 s1 已保存输出上限 32000，用户在编辑入口清空模型输出上限字段并提交
- **THEN** s1 的输出上限从档案移除，详情不再展示该项；同一次提交中对其他元数据字段的修改仍然生效

#### Scenario: 清空输入模态生效
- **WHEN** 档案 s1 已保存输入模态 `text,image`，用户在编辑入口清空输入模态字段并提交
- **THEN** s1 的输入模态从档案移除，详情不再展示该项

#### Scenario: 修改接入地址
- **WHEN** 用户执行 `senv ai provider edit main --base-url https://new.example.com/v1`
- **THEN** 档案的 base_url 更新为归一后的形态，模型集与凭据引用保持不变

#### Scenario: 轮换自有凭据
- **WHEN** 用户在 TTY 执行 edit 并输入新的 API key
- **THEN** `text:llm-keys/<alias>` 被新值覆盖，档案仍引用同一凭据条目，输出不含明文

#### Scenario: 改为外部引用
- **WHEN** 用户执行 `senv ai provider edit main --key-ref env:llm/KEY`
- **THEN** 档案改引用外部条目，原自有凭据 `text:llm-keys/main` 被删除

#### Scenario: 别名不可改
- **WHEN** 用户尝试通过 edit 改变别名
- **THEN** 命令不提供该参数并报错提示，档案与各 agent 配置中的 `senv-<alias>` 标识均不变

#### Scenario: 校验失败不留部分更新
- **WHEN** edit 提供的 `--default-model` 不属于最终模型集
- **THEN** 命令以非 0 退出，档案、凭据与引用全部保持编辑前状态
