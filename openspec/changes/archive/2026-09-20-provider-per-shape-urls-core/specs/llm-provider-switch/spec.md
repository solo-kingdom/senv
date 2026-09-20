## ADDED Requirements

### Requirement: 形态地址门禁

`senv ai switch` SHALL 在写配置前判定目标 agent 协议族（Anthropic 族：claude-code；OpenAI 兼容族：codex/kimi/pi/opencode）的兼容性：目标族存在显式形态地址（Anthropic 族为档案 `anthropic_base_url` 非空；OpenAI 兼容族为 `chat_base_url` 或 `responses_base_url` 任一非空）时 SHALL 放行，MUST NOT 受档案 `api_shape` 声明影响；目标族无显式形态地址时 SHALL 维持既有判定（`api_shape` 未声明放行；声明族与目标族不兼容时 MUST 拒绝且不写任何文件）。拒绝时错误信息 SHALL 给出三个可行动作：改档案形态、补配该族形态地址（`--shape-url <api_shape>=<url>`）、换 provider。形态地址的存在性 MUST NOT 反向决定 OpenAI 族内的线协议选择；线协议仍由档案 `api_shape`（openai-* 声明）或 agent 既有默认决定。MUST NOT 从 URL 内容推断形态。

#### Scenario: 显式 anthropic 地址放行 openai-chat 声明

- **WHEN** 档案 `api_shape` 为 `openai-chat` 且 `anthropic_base_url` 已设置，用户执行 `senv ai switch claude-code <provider>`
- **THEN** 切换成功，claude-code 写入 `anthropic_base_url` 的原样值

#### Scenario: 无显式地址维持拒绝

- **WHEN** 档案 `api_shape` 为 `openai-chat`、三个形态地址均未设置，用户切换 claude-code
- **THEN** 命令以非 0 退出，错误给出改档案形态、补 `--shape-url`、换 provider 三个动作，不写任何文件

#### Scenario: OpenAI 族地址存在即放行 anthropic 声明

- **WHEN** 档案 `api_shape` 为 `anthropic` 且 `responses_base_url` 已设置，用户切换 codex
- **THEN** 门禁放行，codex 线协议维持其既有默认，不因形态地址存在而改变

#### Scenario: 未声明且无显式地址维持旧行为

- **WHEN** 档案 `api_shape` 未设置且三个形态地址均未设置，切换任一 agent
- **THEN** 行为与本要求生效前一致

## MODIFIED Requirements

### Requirement: 接入地址按协议族写回

`senv ai switch` SHALL 先解析本次写回的接入地址，再按目标 agent 的协议族转换后写配置。地址来源 SHALL 为：目标 agent 协议族的显式形态地址存在时优先——Anthropic Messages 族（claude-code）用档案 `anthropic_base_url` 并原样写回（MUST NOT 剥离或改写任何路径段）；OpenAI 兼容族（codex/kimi/pi/opencode）按已解析线协议取对应形态字段（线协议为 chat 取 `chat_base_url`，为 responses 取 `responses_base_url`）。目标族无显式形态地址时回落 `BaseURL` 并按协议族转换：Anthropic 族写不带版本段的形态，OpenAI 兼容族写带末段 `/v1` 的形态。转换 SHALL 在写回前完成且幂等：档案接入地址已归一或未归一的结果一致，重复切换不产生配置漂移。转换 MUST 只处理路径末段并保留 query 与 fragment；解析失败或缺少 scheme/host 时 SHALL 原样写回，由既有校验路径报错。命令成功输出 SHALL 包含实际写入的接入地址及其来源（显式形态地址字段名，或「由 BaseURL 推断」）。本要求 MUST NOT 改变档案在 vault 中的存储值，也 MUST NOT 要求迁移存量档案。

#### Scenario: claude-code 剥离版本段

- **WHEN** 档案接入地址为 `https://api.example.com/v1` 且未设置 `anthropic_base_url`，执行 `senv ai switch claude-code <provider>`
- **THEN** `ANTHROPIC_BASE_URL` 写为 `https://api.example.com`，输出显示该实际写入值与「由 BaseURL 推断」来源

#### Scenario: 显式 anthropic 形态地址原样写回

- **WHEN** 档案 `anthropic_base_url` 为 `https://gw.example.com/api/anthropic`，执行 `senv ai switch claude-code <provider>`
- **THEN** `ANTHROPIC_BASE_URL` 写为 `https://gw.example.com/api/anthropic`（原样，不剥离、不追加版本段），输出显示该值与显式来源

#### Scenario: OpenAI 族显式形态地址优先

- **WHEN** 档案声明 `api_shape=openai-chat` 且 `chat_base_url` 为 `https://chat.example.com/v1`、`BaseURL` 为 `https://gw.example.com/v1`，切换 codex
- **THEN** TOML `base_url` 写为 `https://chat.example.com/v1`，输出显示该值与显式来源

#### Scenario: OpenAI 族字段未设回落 BaseURL

- **WHEN** 档案声明 `api_shape=openai-chat` 且 `chat_base_url` 未设置，切换 codex
- **THEN** 写回 `BaseURL`（带版本段形态），输出显示来源为「由 BaseURL 推断」

#### Scenario: Anthropic 族剥离是无损变换

- **WHEN** 档案接入地址为 `https://api.example.com/v1` 或 `https://api.example.com` 且未设置形态地址
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
