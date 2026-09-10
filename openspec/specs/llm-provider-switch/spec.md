# llm-provider-switch Specification

## Purpose
把「切换 coding agent 的 LLM Provider」从手工改配置文件变成一条 senv 命令：以本机指针记录每个 agent 当前指向，切换时从 vault 解密凭据并按 agent 原生格式原子写回配置，失败不留半写状态。
## Requirements
### Requirement: 切换 agent 指向
`senv ai switch <agent> <provider>` SHALL 校验 agent 属于支持的注册表（claude-code、codex、zcode、kimi、pi、opencode）且 provider 档案存在；`--model` 省略时取档案 `default_model`（为空且模型集恰有一个时取该模型，否则报错要求显式指定）；`--model` 显式给出时必须属于档案模型集。校验通过后 SHALL 解密凭据引用并调用该 agent 的适配器写回配置，再更新本机指针。

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

### Requirement: 原子写回与失败回滚
适配器写回 SHALL 保留目标配置文件中与本功能无关的既有内容（merge 而非整文件重写）；写入 SHALL 以临时文件+重命名完成原子替换，替换前对原文件留备份（`<config>.senv-bak`）。指针更新发生在配置写回成功之后；指针更新失败时 SHALL 从备份恢复配置，保证指针与配置一致。

#### Scenario: merge 保留既有内容
- **WHEN** 目标配置已含与本功能无关的键且执行切换
- **THEN** 写回后这些键原样保留，仅本功能负责的字段被更新

#### Scenario: 写回失败不留半写
- **WHEN** 适配器写回过程出错（序列化、重命名等）
- **THEN** 原配置保持不变（或从备份恢复），指针不变，命令以非 0 退出

#### Scenario: 指针更新失败回滚配置
- **WHEN** 配置已写回但指针保存失败
- **THEN** 配置从备份恢复为切换前内容，指针不变，命令以非 0 退出

### Requirement: 本机指针存储
指针 SHALL 存于 `~/.config/senv/agent-pointers.json`（权限 0600），按 agent id 记录 `(provider, model, switched_at)`。指针是本机状态：MUST NOT 写入 vault，MUST NOT 随 vault 同步。`senv ai status` 与 switch 输出以指针为唯一事实源，不解析 agent 配置文件推断状态。

#### Scenario: 指针落盘
- **WHEN** 切换成功
- **THEN** `~/.config/senv/agent-pointers.json` 记录该 agent 的新指向且权限为 0600

#### Scenario: 指针不进 vault
- **WHEN** 切换成功后查看 vault 数据目录
- **THEN** 不存在任何指针相关条目；vault 同步不携带指针

### Requirement: 查看各 agent 当前指向
`senv ai status` SHALL 列出全部注册 agent：已切换的显示 `(provider, model)` 与切换时间；未切换的显示未切换；cursor 显示不支持。每行 SHALL 附带该 agent 的配置文件路径。

#### Scenario: 混合状态展示
- **WHEN** claude-code 已切换、opencode 未切换、cursor 不支持且用户执行 status
- **THEN** 三行分别显示指向+时间、未切换、不支持，且各附配置路径

#### Scenario: 无指针文件
- **WHEN** 从未执行过切换且用户执行 status
- **THEN** 全部 agent 显示未切换，命令退出码为 0

### Requirement: 凭据按 agent 格式落地
切换时 SHALL 从 vault 解密凭据引用（`text:` 经 text manager、`env:` 经 env manager）并按 agent 原生格式写入配置：支持文件内凭据字段的 agent（claude-code、zcode、kimi、pi、opencode）直接写入并收敛文件权限为 0600；codex 的 TOML 配置只写 `model`/`model_provider`/`base_url`/`env_key`，凭据 MUST NOT 写入文件，命令输出 SHALL 提示通过 senv 的 env 能力把被引用 key 暴露给 codex 环境。

#### Scenario: 文件内凭据 agent
- **WHEN** 切换 pi 至某 provider
- **THEN** `~/.pi/agent/models.json` 中对应 provider 含解密后的 apiKey，文件权限为 0600

#### Scenario: codex 凭据不落盘
- **WHEN** 切换 codex 至某 provider
- **THEN** `config.toml` 含模型与 provider 定义及 env_key 名，不含任何密钥明文，输出含凭据暴露指引

### Requirement: 切换需要解锁 vault
`senv ai switch` SHALL 在 vault 未初始化或未解锁时遵循既有认证流程（提示口令）；指针读取与 `senv ai status` 不需要 vault。

#### Scenario: 锁定状态执行 switch
- **WHEN** vault 已初始化但锁定且用户执行 switch
- **THEN** 提示输入口令，认证通过后完成切换

#### Scenario: status 无需解锁
- **WHEN** vault 锁定且用户执行 status
- **THEN** 正常显示指针状态，不提示口令

