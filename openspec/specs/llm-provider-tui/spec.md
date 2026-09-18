# llm-provider-tui Specification

## Purpose
把 provider 档案浏览与 agent 切换纳入 `senv tui` 全屏界面：浏览时不得泄露凭据，切换复用 agents 子 change 的 SwitchManager（原子写 + 指针 + 回滚），让用户不离开 TUI 即可完成「哪个 agent 用哪个 provider 的哪个模型」。
## Requirements
### Requirement: AI Tab 注册
`senv tui` 在 vault 解锁后 SHALL 注册 AI Tab；`tui.Managers` 的 LLM 管理器为 nil（如 git 模式）时 SHALL 跳过注册且不影响其他 Tab。

#### Scenario: 已解锁进入 TUI
- **WHEN** 用户解锁 vault 后启动 `senv tui`
- **THEN** Tab 栏出现 AI Tab，可切入浏览

#### Scenario: git 模式无 vault
- **WHEN** LLM 管理器为 nil 时启动 TUI
- **THEN** AI Tab 不注册，TUI 正常启动无报错

### Requirement: 浏览 provider 与当前指向
AI Tab SHALL 采用两栏布局：左栏为 provider 列表（别名、默认模型、模型数、被哪些 agent 指向的标记），右栏为 agent 列表（agent id、当前指向 `provider / model`、未切换/不支持）。`←→/hl` SHALL 在左右栏之间切换焦点并给出可见高亮，`↑↓` SHALL 只作用于当前焦点栏。provider 详情（base_url、模型集、凭据引用、`api_shape`、目录来源、各模型已保存的 context window / 输出上限 / 推理档位 / 默认推理档 / 输入模态）SHALL 由 `enter` 打开的详情弹层展示，列表行 SHALL 按截断规则显示，MUST NOT 因长值折行或撑高面板。浏览视图 MUST NOT 展示凭据明文。

#### Scenario: 浏览列表与详情
- **WHEN** 存在档案 main（default m1、3 个模型）且用户进入 AI Tab
- **THEN** 左栏出现 main 行，右栏显示各 agent 的当前指向；按 `enter` 弹出详情层显示 base_url、模型集、凭据引用与 `api_shape`，且不出现任何 key 明文

#### Scenario: 详情展示默认推理档与输入模态
- **WHEN** 档案为某模型保存了默认推理档与输入模态且用户按 `enter`
- **THEN** 详情弹层的模型列表展示这两项

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

### Requirement: 凭据安全
AI Tab 全程 MUST NOT 在渲染文本中输出凭据明文；切换所需的凭据解密 SHALL 仅在 SwitchManager 内部完成，不进入 TUI 状态；新建凭据的遮蔽输入 MUST NOT 被写入任何渲染文本、日志或提示条。

#### Scenario: 全界面无明文
- **WHEN** 用户在 AI Tab 内浏览并完成任意操作
- **THEN** 界面渲染与状态中均不含 key 明文（凭据引用文本除外）

#### Scenario: 遮蔽输入不落渲染
- **WHEN** 用户通过遮蔽输入新建凭据并提交成功
- **THEN** 成功提示只包含别名与来源类型，不含任何 key 片段

### Requirement: AI Tab 档案写操作
AI Tab SHALL 提供 provider 档案的写操作：`n` 新建（读取表单字段后调用 `AddProvider`）、`e` 编辑选中档案（别名只读，调用 `EditProvider`）、`r` 重命名选中档案（表单只收集新别名，调用与 CLI 相同的 rename 语义）、`d` 删除（确认后调用 `RemoveProvider`，沿用自有凭据处理语义）。表单 SHALL 包含模型 context window 字段（格式 `<model>=<tokens>`）、模型输出上限字段（格式 `<model>=<tokens>`）、模型推理档位字段（格式 `<model>=<effort>[;<effort>...]`）、默认推理档字段（格式 `<model>=<effort>` 或集合级单一档位）与输入模态字段（格式 `<model>=<mod>[,<mod>...]`）；编辑表单 SHALL 用档案既有元数据预填这些字段。编辑表单中某个元数据字段被清空后提交，SHALL 等价于通过 `EditProvider` 显式清空该元数据：档案对应条目被移除、详情不再展示，MUST NOT 回填编辑前旧值。新建或改动模型集/元数据时缺失 context window，或某模型已填推理档位但缺默认推理档，SHALL 经统一提示条回显并保持在表单内修正。provider 详情 SHALL 在模型列表中展示已保存的 context window、输出上限、推理档位、默认推理档与输入模态。写操作 SHALL 记入操作审计（`op_llm_provider`），失败 SHALL 经统一提示条回显且不改变既有档案。重命名成功后左栏 SHALL 刷新到新别名，提示可含受影响指针数与「需重跑 switch」提醒，MUST NOT 含凭据明文。多选状态下 `r` SHALL 要求收窄到单选（与既有单选写动词一致）。

#### Scenario: TUI 新建档案
- **WHEN** 用户在 AI Tab 按 `n` 并填写别名、base_url、模型集、模型 context window 与凭据来源后提交
- **THEN** 调用 `AddProvider` 保存档案，左栏出现新档案，提示成功

#### Scenario: TUI 缺少模型 context window
- **WHEN** 新建自定义模型但表单未提供对应 context window，且目录元数据也不存在
- **THEN** 提交失败，表单聚焦模型上下文字段并显示 `--model-context` 指引，不创建档案或凭据

#### Scenario: TUI 设置模型输出与推理
- **WHEN** 用户在新建表单的模型输出字段填 `s1=32000`、模型推理字段填 `s1=low;high`、默认推理档字段填 `s1=high` 后提交成功
- **THEN** 档案 `model_info` 保存 s1 的输出上限、推理档位与默认推理档，详情展示这三项

#### Scenario: TUI 有档位但缺少默认推理档
- **WHEN** 新建表单为某模型填了推理档位但未填默认推理档，且目录也无法提供
- **THEN** 提交失败，表单聚焦默认推理档字段并提示需声明，不创建档案或凭据

#### Scenario: TUI 设置输入模态
- **WHEN** 用户在新建表单的输入模态字段填 `s1=text,image` 后提交成功
- **THEN** 档案保存 s1 的输入模态，详情展示该项

#### Scenario: TUI 清空默认推理档
- **WHEN** 档案 s1 已有默认推理档 `high`，用户按 `e` 打开编辑表单、清空默认推理档字段并提交
- **THEN** `EditProvider` 移除 s1 的默认推理档，详情不再展示，重新打开编辑表单时该字段为空，旧值 `high` 不再出现

#### Scenario: TUI 清空输入模态与输出上限
- **WHEN** 用户在编辑表单同时清空输入模态与模型输出上限字段并提交成功
- **THEN** 档案中这两项元数据均被移除，详情不再展示，且同次提交中其他字段的修改不受影响

#### Scenario: TUI 编辑档案
- **WHEN** 用户按 `e` 修改选中档案的 base_url 或默认模型并提交
- **THEN** 调用 `EditProvider`，档案更新，别名与凭据引用语义按后端规则保持不变或更新

#### Scenario: TUI 重命名档案
- **WHEN** 用户在 provider 栏对选中档案按 `r`，输入可用新别名并提交
- **THEN** 按 rename 语义改名（含自有凭据与指针联动），左栏显示新别名，提示成功且不含凭据明文

#### Scenario: TUI 重命名冲突
- **WHEN** 用户按 `r` 输入的新别名已存在
- **THEN** 表单内联报错且不写入，原档案与凭据不变

#### Scenario: TUI 删除档案
- **WHEN** 用户对选中档案按 `d` 并确认
- **THEN** 调用 `RemoveProvider`，按自有凭据语义处理凭据并删除档案，左栏刷新

#### Scenario: 写操作可审计
- **WHEN** 用户在 TUI 完成一次 provider 新建、重命名或删除
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

### Requirement: AI Tab 可编辑档案说明
新建与编辑 Provider 表单 SHALL 包含说明字段（可选）。详情 SHALL 展示档案说明。说明超限时 SHALL 留在表单内修正，不写档案。

#### Scenario: TUI 新建含说明
- **WHEN** 用户在 AI Tab 新建档案并填写说明后提交成功
- **THEN** 档案保存该说明，详情可见

#### Scenario: TUI 清空说明
- **WHEN** 用户编辑表单清空说明并提交
- **THEN** 档案说明变为空，其他字段不受影响

