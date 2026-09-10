# llm-provider-switch Delta

## MODIFIED Requirements

### Requirement: 切换 agent 指向
`senv ai switch <agent> <provider>` SHALL 校验 agent 属于支持的注册表（claude-code、codex、zcode、kimi、pi、opencode）且 provider 档案存在；`--model` 省略时取档案 `default_model`（为空且模型集恰有一个时取该模型，否则报错要求显式指定）；`--model` 显式给出时必须属于档案模型集。切换前 SHALL 校验档案 `api_shape`（若声明）与目标 agent 协议族的兼容性，不兼容时 MUST 拒绝且不写任何文件。校验通过后 SHALL 解密凭据引用并调用该 agent 的适配器写回配置，再更新本机指针。对已指向同一 provider 的 agent，以新 `--model` 重跑切换 SHALL 仅更换模型，provider 指向与配置中的其它字段保持不变。TUI SHALL 提供等价的「仅换模型」入口。

#### Scenario: 切换成功
- **WHEN** 用户执行 `senv ai switch claude-code myprovider --model m1` 且档案存在、m1 属于模型集
- **THEN** agent 配置被写为该 provider 的 base_url/凭据/模型，指针记录 `(myprovider, m1)`，输出切换结果

#### Scenario: agent 不在注册表
- **WHEN** `<agent>` 为 cursor 或其他未注册 id
- **THEN** 命令以非 0 退出并说明该 agent 不受支持，不写任何文件

#### Scenario: provider 档案不存在
- **WHEN** `<provider>` 在 vault 中无档案
- **THEN** 命令以非 0 退出并提示先执行 `senv ai provider add`，不写任何文件

#### Scenario: 模型不属于档案
- **WHEN** `--model` 不在档案模型集
- **THEN** 命令以非 0 退出并列出可用模型，不写任何文件

#### Scenario: 模型缺省且无法推断
- **WHEN** 省略 `--model` 且档案无 default_model、模型集多于一个
- **THEN** 命令以非 0 退出并要求显式指定 `--model`，不写任何文件

#### Scenario: 接入形态不兼容
- **WHEN** 档案 `api_shape` 为 `openai-chat` 且用户执行 `senv ai switch claude-code <provider>`
- **THEN** 命令以非 0 退出，说明形态与 agent 协议族不兼容并给出「改档案形态或换 provider」两个动作，不写任何文件

#### Scenario: 仅更换模型
- **WHEN** claude-code 当前指向 `myprovider / m1`，用户执行 `senv ai switch claude-code myprovider --model m2`
- **THEN** 配置与指针的模型变为 m2，provider、接入地址与凭据引用保持不变

#### Scenario: TUI 仅换模型
- **WHEN** 用户在 AI Tab 对右栏已指向某 provider 的 agent 按 `m` 并选择同档案的另一个模型
- **THEN** 指针与配置文件中的模型更新，provider 指向不变，成功后刷新指针展示
