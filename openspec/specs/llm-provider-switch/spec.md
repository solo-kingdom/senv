# llm-provider-switch Specification

## Purpose
把「切换 coding agent 的 LLM Provider」从手工改配置文件变成一条 senv 命令：以本机指针记录每个 agent 当前指向，切换时从 vault 解密凭据并按 agent 原生格式原子写回配置，失败不留半写状态。
## Requirements

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

### Requirement: 原子写回与失败回滚
适配器写回 SHALL 保留目标配置中与本功能无关的既有内容，并保持 JSON/TOML 语法有效：本功能 MUST NOT 把值插入多行 scalar、多行数组或错误表块。涉及一个 agent 的多份配置时 SHALL 作为一次事务处理：任一文件失败时，已成功的文件 SHALL 恢复到切换前内容。每次写回 SHALL 使用临时文件加同目录 rename 原子替换，替换前在安全备份中保存原内容；恢复 MUST NOT 覆盖唯一好备份。同一 agent 配置路径的切换 SHALL 串行执行。切换完全成功后 SHALL 清理本功能创建的备份；切换失败且无法恢复时 SHALL 保留可用备份并在错误中说明。

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
- **THEN** 全部已写回配置恢复为切换前内容，指针不变，命令以非 0 退出

#### Scenario: 恢复失败保留好备份
- **WHEN** 指针更新失败且恢复操作也失败
- **THEN** 磁盘上保留切换前内容的有效备份，错误说明恢复失败，命令以非 0 退出

#### Scenario: 并发切换串行化
- **WHEN** CLI、TUI 或 MCP 同时对同一 agent 发起两次切换
- **THEN** 两次切换按路径串行完成，最终配置和指针只反映后完成的一次，不出现交叉写或备份互相覆盖

#### Scenario: 成功后清理备份
- **WHEN** 切换、指针更新和恢复检查全部成功
- **THEN** 本功能为本次切换创建的 `.senv-bak` 备份被删除，新配置和指针保留

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

### Requirement: 接入地址按协议族写回
`senv ai switch` SHALL 按目标 agent 的协议族转换档案接入地址后再写配置：Anthropic Messages 族（claude-code）写不带版本段的形态，OpenAI 兼容族（codex/kimi/pi/opencode）写带末段 `/v1` 的形态。转换 SHALL 在写回前完成且幂等：档案接入地址已归一或未归一的结果一致，重复切换不产生配置漂移。转换 MUST 只处理路径末段并保留 query 与 fragment；解析失败或缺少 scheme/host 时 SHALL 原样写回，由既有校验路径报错。命令成功输出 SHALL 包含实际写入的接入地址。本要求 MUST NOT 改变档案在 vault 中的存储值，也 MUST NOT 要求迁移存量档案。

#### Scenario: claude-code 剥离版本段
- **WHEN** 档案接入地址为 `https://api.example.com/v1` 且执行 `senv ai switch claude-code <provider>`
- **THEN** `ANTHROPIC_BASE_URL` 写为 `https://api.example.com`，输出显示该实际写入值

#### Scenario: Anthropic 族剥离是无损变换
- **WHEN** 档案接入地址为 `https://api.example.com/v1` 或 `https://api.example.com`
- **THEN** claude-code 最终请求的 URL 均为 `https://api.example.com/v1/messages`

#### Scenario: OpenAI 兼容族补版本段
- **WHEN** 存量档案接入地址为 `https://api.example.com`（无版本段）且执行 `senv ai switch codex <provider>`
- **THEN** TOML `base_url` 写为 `https://api.example.com/v1`，输出显示该实际写入值

#### Scenario: 带路径前缀的接入地址
- **WHEN** 档案接入地址为 `https://api.example.com/api/llm/v1` 且执行 `senv ai switch claude-code <provider>`
- **THEN** 写为 `https://api.example.com/api/llm`，中间路径段不被改动

#### Scenario: query 与 fragment 保留
- **WHEN** 档案接入地址为 `https://api.example.com/v1?key=abc`
- **THEN** OpenAI 兼容族写回后 query 仍在，Anthropic 族剥离版本段后 query 仍随 base 保留

#### Scenario: 非 v1 版本段不被猜测
- **WHEN** 档案接入地址为 `https://api.example.com/v1beta`
- **THEN** Anthropic 族原样写回，OpenAI 兼容族补为 `https://api.example.com/v1beta/v1`，不做协议探测

#### Scenario: 重复切换幂等
- **WHEN** 对同一 agent 连续执行两次相同 `switch`
- **THEN** 第二次写回的接入地址与第一次相同，配置无漂移
