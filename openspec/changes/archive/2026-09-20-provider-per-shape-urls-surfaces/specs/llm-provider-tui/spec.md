## ADDED Requirements

### Requirement: AI Tab 形态地址表单与详情

TUI AI Tab 的 provider 表单（新建与编辑）SHALL 提供三个形态地址字段：`chat_base_url`、`responses_base_url`、`anthropic_base_url`，语义与 CLI `--shape-url` 一致（留空 = 不设置/清除；`anthropic_base_url` 不做版本段归一）。编辑表单 SHALL 用档案既有值预填；某形态地址字段被清空后提交，SHALL 等价于显式清除该字段，MUST NOT 回填旧值。非法取值（非 https 且未走既有 HTTP 门禁、空 host、userinfo）SHALL 内联报错且不写入。provider 详情 SHALL 展示三个形态地址（未设显示 `-`），MUST NOT 含凭据明文。表单字段说明 SHALL 提示 anthropic 地址是 claude-code 拼 `/v1/messages` 的 base，应填 root 而非带 `/v1` 的端点。

#### Scenario: TUI 设置形态地址

- **WHEN** 用户在新建表单的形态地址字段填 `https://gw.example.com/api/anthropic` 并提交成功
- **THEN** 档案保存 `anthropic_base_url`，详情展示该项

#### Scenario: TUI 清空形态地址

- **WHEN** 档案已设 `chat_base_url`，用户按 `e` 清空该字段并提交
- **THEN** 档案中该字段被移除，详情不再展示，重新打开编辑表单时该字段为空

#### Scenario: TUI 非法形态地址内联报错

- **WHEN** 用户在表单填入含 userinfo 的形态地址并提交
- **THEN** 表单内联报错且不写入，原档案不变

#### Scenario: TUI 详情展示形态地址

- **WHEN** 档案设置了部分形态地址，用户按 `enter` 打开详情
- **THEN** 详情展示已设形态地址，未设项显示 `-`，不含凭据明文
