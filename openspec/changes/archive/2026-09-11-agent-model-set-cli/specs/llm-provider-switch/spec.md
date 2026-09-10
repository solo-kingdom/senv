## ADDED Requirements

### Requirement: 切换命令参数
`senv ai switch <agent> <provider>` SHALL 接受 `--models`（逗号分隔、可重复；省略时取 Provider 模型集全集，显式给出时保序）与 `--default-model`（省略时取档案默认模型）。`--model` MUST NOT 被接受：出现时命令 MUST 以非 0 退出并提示改用 `--models` 与 `--default-model`。参数校验 MUST 在任何文件写入之前完成，失败时不改任何文件。成功输出 SHALL 包含 provider、Agent 模型集条数与默认模型。审计事件 SHALL 记录默认模型与模型集条数，且 MUST NOT 包含任何凭据材料或值。

#### Scenario: 省略 --models 即全选
- **WHEN** 用户执行 `senv ai switch claude-code myprovider` 且档案模型集为 m1、m2
- **THEN** 本次 Agent 模型集为 m1、m2，输出显示条数为 2 与默认模型

#### Scenario: --models 显式给定保序
- **WHEN** 用户执行 `--models m2,m1`
- **THEN** Agent 模型集按 m2、m1 的顺序写入

#### Scenario: --model 被拒绝并提示
- **WHEN** 用户执行 `senv ai switch claude-code myprovider --model m1`
- **THEN** 命令以非 0 退出，提示改用 `--models` 与 `--default-model`，不写任何文件

#### Scenario: --default-model 覆盖本次
- **WHEN** 档案默认模型为 m1，用户执行 `--default-model m2`（m2 在 Agent 模型集内）
- **THEN** 本次默认模型为 m2，档案的默认模型保持不变

#### Scenario: 审计记录模型数与默认模型
- **WHEN** 切换成功
- **THEN** 审计事件 detail 含默认模型与模型集条数，不含凭据材料

## MODIFIED Requirements

### Requirement: 查看各 agent 当前指向
`senv ai status` SHALL 列出全部注册 agent：已切换的显示 `provider / 默认模型（N 个模型）` 与切换时间；指针记录的 Agent 模型集与档案当前模型集不一致时 SHALL 附带漂移提示，且 MUST NOT 通过解析 agent 配置文件来判定；未切换的显示未切换；cursor 显示不支持。每行 SHALL 附带该 agent 的配置文件路径。

#### Scenario: 混合状态展示
- **WHEN** claude-code 已切换、opencode 未切换、cursor 不支持且用户执行 status
- **THEN** 三行分别显示指向（含默认模型与模型条数）加时间、未切换、不支持，且各附配置路径

#### Scenario: 无指针文件
- **WHEN** 从未执行过切换且用户执行 status
- **THEN** 全部 agent 显示未切换，命令退出码为 0

#### Scenario: 漂移提示
- **WHEN** 指针记录 `provider P / models [m1, m2]`，而档案 P 的模型集已变为 `[m1]`
- **THEN** 该行附带漂移提示，说明与档案不一致，且不读取 agent 配置文件
