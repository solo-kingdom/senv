# llm-provider-tui Delta

## MODIFIED Requirements

### Requirement: 浏览 provider 与当前指向
AI Tab SHALL 采用两栏布局：左栏为 provider 列表（别名、默认模型、模型数、被哪些 agent 指向的标记），右栏为 agent 列表（agent id、当前指向 `provider / model`、未切换/不支持）。`←→/hl` SHALL 在左右栏之间切换焦点并给出可见高亮，`↑↓` SHALL 只作用于当前焦点栏。provider 详情（base_url、模型集、凭据引用、`api_shape`、目录来源）SHALL 由 `enter` 打开的详情弹层展示，列表行 SHALL 按截断规则显示，MUST NOT 因长值折行或撑高面板。浏览视图 MUST NOT 展示凭据明文。

#### Scenario: 浏览列表与详情
- **WHEN** 存在档案 main（default m1、3 个模型）且用户进入 AI Tab
- **THEN** 左栏出现 main 行，右栏显示各 agent 的当前指向；按 `enter` 弹出详情层显示 base_url、模型集、凭据引用与 `api_shape`，且不出现任何 key 明文

#### Scenario: 焦点切换生效
- **WHEN** 用户在 AI Tab 按 `→/l` 再按 `↓/j`
- **THEN** 焦点移到右栏且高亮指示跟随，光标在 agent 列表中下移，provider 选择不变

#### Scenario: 长值不折行
- **WHEN** 某 provider 的模型集长度超过右栏宽度
- **THEN** 左栏该行按宽度截断，面板行数与高度不变，完整内容在详情弹层可见

#### Scenario: 无档案
- **WHEN** vault 中无任何 provider 档案
- **THEN** Tab 正常渲染空态提示，引导执行 `senv ai provider add`

### Requirement: Tab 内切换操作
AI Tab SHALL 提供切换与换模型键位：`s` 以左栏选中的 provider 为目标，对右栏选中的 agent 执行切换（选择该 provider 模型集中的模型后确认）；`m` 对右栏已指向某 provider 的 agent 仅更换模型（provider 不变）；两者均复用 SwitchManager 的原子写回与回滚。成功后 SHALL 刷新指针展示并提示结果（codex 场景 SHALL 提示需暴露的环境变量名）；失败 SHALL 经统一提示条回显原因且指针与配置不变。

#### Scenario: 切换成功
- **WHEN** 用户对 provider main 按 `s` 并在右栏选中 claude-code，选择模型 m1 后确认
- **THEN** TUI 调用 SwitchManager 成功，agent 行立即显示 `main / m1`，出现成功提示

#### Scenario: 切换失败回显
- **WHEN** 切换执行失败（如目标配置目录不可写）
- **THEN** 提示条显示失败原因，agent 行与配置保持原状

#### Scenario: codex 切换提示
- **WHEN** 用户切换 codex 至某 provider
- **THEN** 成功提示包含需设置的环境变量名（如 `SENV_MAIN_API_KEY`）

#### Scenario: 仅换模型无未指向报错
- **WHEN** 用户对未指向任何 provider 的 agent 按 `m`
- **THEN** 界面提示先执行切换（`s`），不调用 SwitchManager


### Requirement: 凭据安全
AI Tab 全程 MUST NOT 在渲染文本中输出凭据明文；切换所需的凭据解密 SHALL 仅在 SwitchManager 内部完成，不进入 TUI 状态；新建凭据的遮蔽输入 MUST NOT 被写入任何渲染文本、日志或提示条。

#### Scenario: 全界面无明文
- **WHEN** 用户在 AI Tab 内浏览并完成任意操作
- **THEN** 界面渲染与状态中均不含 key 明文（凭据引用文本除外）

#### Scenario: 遮蔽输入不落渲染
- **WHEN** 用户通过遮蔽输入新建凭据并提交成功
- **THEN** 成功提示只包含别名与来源类型，不含任何 key 片段

## ADDED Requirements

### Requirement: AI Tab 档案写操作
AI Tab SHALL 提供 provider 档案的写操作：`n` 新建（读取表单字段后调用 `AddProvider`）、`e` 编辑选中档案（别名只读，调用 `EditProvider`）、`d` 删除（确认后调用 `RemoveProvider`，沿用自有凭据处理语义）。写操作 SHALL 记入操作审计（`op_llm_provider`），失败 SHALL 经统一提示条回显且不改变既有档案。

#### Scenario: TUI 新建档案
- **WHEN** 用户在 AI Tab 按 `n` 并填写别名、base_url、模型集与凭据来源后提交
- **THEN** 调用 `AddProvider` 保存档案，左栏出现新档案，提示成功

#### Scenario: TUI 编辑档案
- **WHEN** 用户按 `e` 修改选中档案的 base_url 或默认模型并提交
- **THEN** 调用 `EditProvider`，档案更新，别名与凭据引用语义按后端规则保持不变或更新

#### Scenario: TUI 删除档案
- **WHEN** 用户对选中档案按 `d` 并确认
- **THEN** 调用 `RemoveProvider`，按自有凭据语义处理凭据并删除档案，左栏刷新

#### Scenario: 写操作可审计
- **WHEN** 用户在 TUI 完成一次 provider 新建或删除
- **THEN** 审计中出现 `op_llm_provider` 事件，Audit Tab 可查到，且不含凭据明文

### Requirement: 凭据录入
AI Tab SHALL 支持两种凭据来源：选择既有 vault 条目（`env:<group>/<key>` 或 `text:<group>/<key>` 的引用选择器）与遮蔽输入新建自有凭据（`text:llm-keys/<alias>`）。遮蔽输入 SHALL 以掩码回显，明文 MUST NOT 进入 TUI 状态、提示文本或渲染输出；提交后仅保存引用。

#### Scenario: 选择既有条目
- **WHEN** 用户在凭据字段选择已有 `env:llm/KEY` 条目
- **THEN** 档案保存为外部引用，不创建 `llm-keys/<alias>` 条目

#### Scenario: 遮蔽输入新建凭据
- **WHEN** 用户选择新建凭据并输入 API key
- **THEN** 输入以掩码回显，提交后 `text:llm-keys/<alias>` 保存密文，TUI 不再持有明文

#### Scenario: 全界面无明文
- **WHEN** 用户在 AI Tab 内浏览并完成任意操作
- **THEN** 界面渲染与状态中均不含 key 明文（凭据引用文本除外）
