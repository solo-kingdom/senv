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
AI Tab SHALL 双栏展示：左栏为 provider 列表（别名、默认模型、模型数），右栏展示选中 provider 详情（base_url、模型集、凭据引用）与各 agent 当前指针（复用 `senv ai status` 的行语义：已切换/未切换/不支持）。浏览视图 MUST NOT 展示凭据明文。

#### Scenario: 浏览列表与详情
- **WHEN** 存在档案 main（default m1、3 个模型）且用户进入 AI Tab
- **THEN** 左栏出现 main 行，右栏显示 base_url、模型集、凭据引用与各 agent 指针，且不出现任何 key 明文

#### Scenario: 无档案
- **WHEN** vault 中无任何 provider 档案
- **THEN** Tab 正常渲染空态提示，引导执行 `senv ai provider add`

### Requirement: Tab 内切换操作
AI Tab SHALL 提供切换键位：选中 provider 后进入 agent 选择（仅受支持 agent），再选择该 provider 模型集中的模型，确认后调用 SwitchManager 执行切换。成功后 SHALL 刷新指针展示并提示结果（codex 场景 SHALL 提示需暴露的环境变量名）；失败 SHALL 在错误横幅展示原因且指针与配置不变。

#### Scenario: 切换成功
- **WHEN** 用户对 provider main 执行切换并选择 claude-code / m1
- **THEN** TUI 调用 SwitchManager 成功，指针区 claude-code 行立即显示 `main / m1`，出现成功提示

#### Scenario: 切换失败回显
- **WHEN** 切换执行失败（如目标配置目录不可写）
- **THEN** 错误横幅显示失败原因，指针区保持原状

#### Scenario: codex 切换提示
- **WHEN** 用户切换 codex 至某 provider
- **THEN** 成功提示包含需设置的环境变量名（如 `SENV_MAIN_API_KEY`）

### Requirement: 凭据安全
AI Tab 全程 MUST NOT 在渲染文本中输出凭据明文；切换所需的凭据解密 SHALL 仅在 SwitchManager 内部完成，不进入 TUI 状态。

#### Scenario: 全界面无明文
- **WHEN** 用户在 AI Tab 内浏览并完成任意操作
- **THEN** 界面渲染与状态中均不含 key 明文（凭据引用文本除外）

