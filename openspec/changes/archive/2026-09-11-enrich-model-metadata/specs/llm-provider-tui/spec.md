## MODIFIED Requirements

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

### Requirement: AI Tab 档案写操作
AI Tab SHALL 提供 provider 档案的写操作：`n` 新建（读取表单字段后调用 `AddProvider`）、`e` 编辑选中档案（别名只读，调用 `EditProvider`）、`d` 删除（确认后调用 `RemoveProvider`，沿用自有凭据处理语义）。表单 SHALL 包含模型 context window 字段（格式 `<model>=<tokens>`）、模型输出上限字段（格式 `<model>=<tokens>`）、模型推理档位字段（格式 `<model>=<effort>[;<effort>...]`）、默认推理档字段（格式 `<model>=<effort>` 或集合级单一档位）与输入模态字段（格式 `<model>=<mod>[,<mod>...]`）；编辑表单 SHALL 用档案既有元数据预填这些字段。新建或改动模型集/元数据时缺失 context window，或某模型已填推理档位但缺默认推理档，SHALL 经统一提示条回显并保持在表单内修正。provider 详情 SHALL 在模型列表中展示已保存的 context window、输出上限、推理档位、默认推理档与输入模态。写操作 SHALL 记入操作审计（`op_llm_provider`），失败 SHALL 经统一提示条回显且不改变既有档案。

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

#### Scenario: TUI 编辑档案
- **WHEN** 用户按 `e` 修改选中档案的 base_url 或默认模型并提交
- **THEN** 调用 `EditProvider`，档案更新，别名与凭据引用语义按后端规则保持不变或更新

#### Scenario: TUI 删除档案
- **WHEN** 用户对选中档案按 `d` 并确认
- **THEN** 调用 `RemoveProvider`，按自有凭据语义处理凭据并删除档案，左栏刷新

#### Scenario: 写操作可审计
- **WHEN** 用户在 TUI 完成一次 provider 新建或删除
- **THEN** 审计中出现 `op_llm_provider` 事件，Audit Tab 可查到，且不含凭据明文
