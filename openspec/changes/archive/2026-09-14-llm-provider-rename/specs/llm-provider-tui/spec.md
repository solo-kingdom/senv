## MODIFIED Requirements

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
