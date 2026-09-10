## MODIFIED Requirements

### Requirement: 切换 agent 指向
`senv ai switch <agent> <provider>` SHALL 校验 agent 属于支持的注册表（claude-code、codex、zcode、kimi、pi、opencode）且 provider 档案存在。切换 SHALL 把 provider 指向、**Agent 模型集**（默认取 Provider 模型集全集，显式给出时取保序子集）与**默认模型**写入该 agent 的原生配置，使该 agent 自己的模型选择器能在集合内切换：claude-code 写 `modelPicker`，codex 写指向 senv 生成 catalog 文件的 `model_catalog_json`，kimi 为每个模型写一条 `[models.*]`，pi 写 `providers.<id>.models[]`，opencode 写 `provider.<id>.models{}`。Agent 模型集 MUST NOT 为空，每个模型 MUST 属于档案模型集；默认模型 MUST 属于 Agent 模型集。切换 SHALL 清理上一次由 senv 写入、本次不再需要的条目与不再被指向的 `senv-<alias>` catalog 文件。切换前 SHALL 校验档案 `api_shape`（若声明）与目标 agent 协议族的兼容性，不兼容时 MUST 拒绝且不写任何文件。校验通过后 SHALL 解密凭据引用并调用该 agent 的适配器写回配置，再更新本机指针。对已指向同一 provider 的 agent，以新默认模型重跑切换 SHALL 仅更换默认模型，Agent 模型集、provider 指向与配置中的其它字段保持不变；以显式模型集重跑 SHALL 按新集合重算并清理差集。TUI SHALL 提供等价的「仅换默认模型」入口。

#### Scenario: 切换成功
- **WHEN** 用户执行 `senv ai switch claude-code myprovider`，档案模型集为 m1、m2
- **THEN** agent 配置写出该 provider 的 base_url/凭据与 `modelPicker`（含 m1、m2，替换内置 lineup）、`model` 为档案默认模型，指针记录 provider 与 `[m1, m2]`

#### Scenario: codex 生成可解析 catalog
- **WHEN** 用户切换 codex 至某 provider
- **THEN** `config.toml` 的 `model_catalog_json` 指向 senv 生成的 `senv-<alias>.json`，该文件列出全部选中模型且能被 codex 解析，文件权限为 0600

#### Scenario: 显式模型集保序
- **WHEN** 用户以显式模型集 `m2,m1` 切换
- **THEN** 配置按 m2、m1 的顺序写入这两个模型，指针记录同一顺序

#### Scenario: 模型集为空
- **WHEN** 用户显式给出的模型集为空
- **THEN** 命令以非 0 退出，不写任何文件

#### Scenario: 模型不属于档案
- **WHEN** 显式模型集包含不属于 Provider 模型集的模型
- **THEN** 命令以非 0 退出并列出可用模型，不写任何文件

#### Scenario: 模型缺省且无法推断
- **WHEN** 档案没有默认模型且用户未显式指定默认模型
- **THEN** 命令以非 0 退出并要求显式指定，不静默取模型集首项，不写任何文件

#### Scenario: 默认模型不属于 Agent 模型集
- **WHEN** 默认模型不在本次 Agent 模型集内
- **THEN** 命令以非 0 退出，不写任何文件

#### Scenario: agent 不在注册表
- **WHEN** `<agent>` 为 cursor 或其他未注册 id
- **THEN** 命令以非 0 退出并说明该 agent 不受支持，不写任何文件

#### Scenario: provider 档案不存在
- **WHEN** `<provider>` 在 vault 中无档案
- **THEN** 命令以非 0 退出并提示先执行 `senv ai provider add`，不写任何文件

#### Scenario: 接入形态不兼容
- **WHEN** 档案 `api_shape` 为 `openai-chat` 且用户执行 `senv ai switch claude-code <provider>`
- **THEN** 命令以非 0 退出，说明形态与 agent 协议族不兼容并给出「改档案形态或换 provider」两个动作，不写任何文件

#### Scenario: 仅更换模型
- **WHEN** claude-code 当前指向 `myprovider` 且 Agent 模型集为 m1、m2，用户以默认模型 m2 重跑切换
- **THEN** 只有默认模型变为 m2，Agent 模型集、provider、接入地址与凭据引用保持不变

#### Scenario: 缩小模型集清理旧条目
- **WHEN** 上一次写入的 Agent 模型集为 m1、m2，本次以 `m1` 重新切换
- **THEN** 配置中只剩 m1 的 senv 条目，m2 对应的 senv 条目被删除

#### Scenario: 换 provider 清理旧痕迹
- **WHEN** 同一 agent 从 provider A 切到 provider B
- **THEN** A 的 senv 条目与不再被指向的 `senv-*.json` catalog 文件被删除，B 的条目与 catalog 写入

#### Scenario: 用户自有配置不被清理
- **WHEN** agent 配置中存在用户自己写的 provider/模型条目或自有 catalog 文件
- **THEN** 它们原样保留：senv 只改自己命名空间与本次必须改的键，不删除自有 catalog 文件

#### Scenario: TUI 仅换模型
- **WHEN** 用户在 AI Tab 对右栏已指向某 provider 的 agent 按 `m` 并在其 Agent 模型集内选择另一个模型
- **THEN** 指针与配置文件中的默认模型更新，Agent 模型集与 provider 指向不变，成功后刷新展示

### Requirement: 原子写回与失败回滚
适配器写回 SHALL 保留目标配置中与本功能无关的既有内容，并保持 JSON/TOML 语法有效：本功能 MUST NOT 把值插入多行 scalar、多行数组或错误表块。涉及一个 agent 的多份配置、本次新建的文件（如 codex catalog）与本次删除的条目或文件时 SHALL 作为一次事务处理：任一环节失败时，已成功的写入 SHALL 恢复到切换前内容，已删除的内容 SHALL 恢复，已新建的文件 SHALL 删除。每次写回 SHALL 使用临时文件加同目录 rename 原子替换，替换前在安全备份中保存原内容；恢复 MUST NOT 覆盖唯一好备份。同一 agent 配置路径的切换 SHALL 串行执行。切换完全成功后 SHALL 清理本功能创建的备份；切换失败且无法恢复时 SHALL 保留可用备份并在错误中说明。

#### Scenario: merge 保留既有内容
- **WHEN** 目标配置已含与本功能无关的键且执行切换
- **THEN** 写回后这些键原样保留，仅本功能负责的字段被更新

#### Scenario: 合法 TOML 不被破坏
- **WHEN** Codex TOML 已含多行数组或多行字符串等合法 TOML 值且执行切换
- **THEN** 新文件仍可被 TOML parser 解析，既有值语义不变，且不产生重复顶层键

#### Scenario: 多文件适配器回滚
- **WHEN** Pi 的第一份配置写回成功但第二份配置写回失败
- **THEN** 第一份配置恢复为切换前内容，第二份保持原状，指针不变，命令非 0 退出

#### Scenario: 写回失败不留半写
- **WHEN** 单文件适配器在序列化、重命名等写回过程出错
- **THEN** 原配置保持不变（或从备份恢复），指针不变，命令以非 0 退出

#### Scenario: 指针更新失败回滚配置
- **WHEN** 配置已写回但指针保存失败
- **THEN** 全部已写回配置恢复为切换前内容，本次新建的 catalog 文件被删除，指针不变，命令以非 0 退出

#### Scenario: 清理失败回滚本次写入
- **WHEN** 本次切换需要删除旧条目或旧 catalog 文件但删除失败
- **THEN** 本次写入的新内容一并回滚，命令以非 0 退出，指针不变

#### Scenario: 恢复失败保留好备份
- **WHEN** 指针更新失败且恢复操作也失败
- **THEN** 磁盘上保留切换前内容的有效备份，错误说明恢复失败，命令以非 0 退出

#### Scenario: 并发切换串行化
- **WHEN** CLI、TUI 或 MCP 同时对同一 agent 发起两次切换
- **THEN** 两次切换按路径串行完成，最终配置和指针只反映后完成的一次，不出现交叉写或备份互相覆盖

#### Scenario: 成功后清理备份
- **WHEN** 切换、指针更新和恢复检查全部成功
- **THEN** 本功能为本次切换创建的 `.senv-bak` 备份被删除，新配置和指针保留
