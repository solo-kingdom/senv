# llm-provider Specification

## Purpose
把 LLM Provider 档案作为 vault 加密资产存储：档案与凭据分离（档案只存凭据引用），模型集可从模型目录自动装配并允许自定义，为后续 agent 切换提供唯一事实源。
## Requirements
### Requirement: 添加 LLM Provider 档案
`senv ai provider add` SHALL 以别名为唯一标识保存档案：校验 base_url 为 http(s) 地址；凭据必须且只能通过 `--api-key`（存入 vault 内保留 text 组 `llm-keys`，档案存引用 `text:llm-keys/<alias>`）或 `--key-ref`（指向既有 env/text entry，格式 `env:<group>/<key>` 或 `text:<group>/<key>`）提供；模型集为 `--catalog-provider` 指向的目录模型与 `--model` 自定义模型的并集且不得为空；`--default-model` 必须属于模型集。

#### Scenario: 目录模型加自定义模型
- **WHEN** 用户执行 add 且给出 `--catalog-provider p1` 与 `--model custom-1`，缓存中 p1 含模型 a、b
- **THEN** 档案保存成功，模型集为 a、b、custom-1，凭据按所选方式存入 vault，输出保存结果

#### Scenario: 目录中无该 provider
- **WHEN** `--catalog-provider` 在模型目录缓存中不存在
- **THEN** 命令以非 0 退出并提示执行 `senv ai refresh`，不写入任何档案或凭据

#### Scenario: 模型集为空
- **WHEN** add 未提供任何模型来源或并集为空
- **THEN** 命令以非 0 退出并报错，不写入任何档案或凭据

#### Scenario: 默认模型不在模型集
- **WHEN** `--default-model` 不在最终模型集中
- **THEN** 命令以非 0 退出并报错，不写入任何档案或凭据

#### Scenario: 别名重复
- **WHEN** 别名已存在且未加 `--force`
- **THEN** 命令以非 0 退出并提示使用 `--force`；加 `--force` 时覆盖旧档案

### Requirement: 查看 LLM Provider 档案
`senv ai provider list` SHALL 列出全部档案的摘要（别名、base_url、模型数、默认模型、目录来源、凭据引用）；`senv ai provider show` SHALL 展示单个档案详情。两者 MUST NOT 输出凭据明文。

#### Scenario: 列出档案
- **WHEN** vault 中存在档案且用户执行 list
- **THEN** 输出每个档案的摘要行且不含凭据明文

#### Scenario: 查看不存在的档案
- **WHEN** `show` 的别名不存在
- **THEN** 命令以非 0 退出并报错

### Requirement: 删除 LLM Provider 档案
`senv ai provider remove` SHALL 删除档案；当凭据引用指向本命令管理的保留组（`text:llm-keys/<alias>`）时同时删除该凭据条目，其他引用（如外部 env entry）保留不动。

#### Scenario: 删除带自有凭据的档案
- **WHEN** 档案凭据为 `text:llm-keys/<alias>` 且执行 remove
- **THEN** 档案与对应凭据条目均被删除，输出两者均已删除

#### Scenario: 删除引用外部凭据的档案
- **WHEN** 档案凭据为外部 `--key-ref` 且执行 remove
- **THEN** 仅删除档案，外部条目保留，输出说明

### Requirement: 档案命令需要解锁 vault
`senv ai provider` 子命令 SHALL 在 vault 未初始化或未解锁时遵循既有认证流程（提示口令），不静默降级为明文存储。

#### Scenario: 未解锁时执行 add
- **WHEN** vault 已初始化但处于锁定状态且用户执行 add
- **THEN** 提示输入口令，认证通过后完成写入

