## ADDED Requirements

### Requirement: AI Tab 切换结果提示完整性
AI Tab 的切换成功提示 SHALL 使用本次切换返回的确切信息：当目标 agent 为 codex 时 SHALL 含写进 `env_key` 的环境变量名，且该名字 MUST 取自切换结果而非固定前缀示例；本次切换返回的全部 warning（凭据组未激活、模型元数据缺失等）SHALL 全部出现在提示中，MUST NOT 只展示第一条。切换失败时 MUST NOT 展示任何凭据暴露提示。

#### Scenario: codex 提示使用实际写入的名字
- **WHEN** 用户切换 codex 至 `credential_ref` 为 `env:ai/DEEPSEEK_API_KEY` 的 provider
- **THEN** 成功提示含 `DEEPSEEK_API_KEY`，不含 `SENV_` 派生的名字

#### Scenario: 多条 warning 全部展示
- **WHEN** 本次切换同时返回「凭据组未激活」与模型元数据缺失两条 warning
- **THEN** 提示中两条 warning 均可见，不是仅第一条

#### Scenario: 非 codex 切换无凭据提示
- **WHEN** 用户切换 pi 至某 provider
- **THEN** 成功提示不含环境变量暴露指引
