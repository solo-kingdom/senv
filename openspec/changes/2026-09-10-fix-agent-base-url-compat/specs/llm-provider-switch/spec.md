## ADDED Requirements

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
