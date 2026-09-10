## MODIFIED Requirements

### Requirement: 本机指针存储
指针 SHALL 存于 `~/.config/senv/agent-pointers.json`（权限 0600），按 agent id 记录 `(provider, models[], default_model, switched_at)`：`models` 是该 agent 配置中实际写入的 Agent 模型集，`default_model` 是其起始模型。读取时若记录只有 `model` 字段（version 1 旧格式）SHALL 视为 `models=[model]`、`default_model=model`，MUST NOT 要求用户重新切换；下一次切换写回时升级为新结构。指针是本机状态：MUST NOT 写入 vault，MUST NOT 随 vault 同步。`senv ai status` 与 switch 输出以指针为唯一事实源，不解析 agent 配置文件推断状态。

#### Scenario: 指针落盘
- **WHEN** 切换成功
- **THEN** `~/.config/senv/agent-pointers.json` 记录该 agent 的 provider、Agent 模型集、默认模型与切换时间，权限为 0600

#### Scenario: 旧指针读作单元素集
- **WHEN** 指针文件是 version 1 格式且某 agent 记录为 `model: m1`
- **THEN** 读取结果视为 `models=[m1]`、`default_model=m1`，命令不报错也不要求重新切换

#### Scenario: 指针不进 vault
- **WHEN** 切换成功后查看 vault 数据目录
- **THEN** 不存在任何指针相关条目；vault 同步不携带指针
