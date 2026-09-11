# llm-provider-tui 增量

## MODIFIED Requirements

### Requirement: Tab 内切换操作

AI Tab SHALL 提供切换与换默认模型键位：`s` 以左栏选中的 provider 为目标，对右栏选中的 agent 执行切换——先多选 Agent 模型集（space 逐个勾选/取消，进入时默认全选 Provider 模型集），再选定默认模型（默认取档案默认模型）后确认；`M` 对右栏已指向某 provider 的 agent 仅更换默认模型（与 `s` 成对，大写为变体语义），候选限定在该 provider 当前写入该 agent 的 Agent 模型集内，不改动模型集；两者均复用 SwitchManager 的原子写回与回滚。模型集为空时 MUST NOT 提交切换。成功后 SHALL 刷新指针展示并提示结果（含模型集条数与默认模型；codex 场景 SHALL 提示需暴露的环境变量名）；失败 SHALL 经统一提示条回显原因且指针与配置不变。

#### Scenario: 切换成功
- **WHEN** 用户对 provider main 按 `s` 并在右栏选中 claude-code，勾选两个模型、选定默认模型后确认
- **THEN** TUI 调用 SwitchManager 成功，agent 行立即显示 `main / <默认模型>（2 个模型）`，出现成功提示

#### Scenario: 默认全选
- **WHEN** 用户进入 `s` 的模型集选择步骤后不做任何勾选调整
- **THEN** 提交的 Agent 模型集等于该 provider 的 Provider 模型集全集

#### Scenario: 空模型集被拦截
- **WHEN** 用户取消勾选全部模型后确认
- **THEN** 界面提示模型集不能为空，不调用 SwitchManager，配置与指针不变

#### Scenario: 切换失败回显
- **WHEN** 切换执行失败（如目标配置目录不可写）
- **THEN** 提示条显示失败原因，agent 行与配置保持原状

#### Scenario: codex 切换提示
- **WHEN** 用户切换 codex 至某 provider
- **THEN** 成功提示包含需设置的环境变量名（如 `SENV_MAIN_API_KEY`）

#### Scenario: 仅换模型无未指向报错
- **WHEN** 用户对未指向任何 provider 的 agent 按 `M`
- **THEN** 界面提示先执行切换（`s`），不调用 SwitchManager

#### Scenario: 仅换模型限定在已写入集合内
- **WHEN** agent 当前 Agent 模型集为 m1、m2，用户按 `M`
- **THEN** 候选只有 m1、m2，选择后只更新默认模型，模型集与 provider 指向不变

#### Scenario: 漂移展示与 status 一致
- **WHEN** 指针中的 Agent 模型集与 provider 档案当前模型集不一致
- **THEN** agent 行附带漂移提示，判定不依赖解析 agent 配置文件
