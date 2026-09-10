## 1. 归一函数 [高优先级]

- [x] 1.1 在 `internal/llm` 新增协议族与接入地址归一函数：OpenAI 兼容族补末段 `/v1`，Anthropic 族剥离末段 `/v1`，两者共用尾斜杠收敛且幂等；解析失败原样返回。验证：`go test ./internal/llm -run BaseURL`。
- [x] 1.2 补 table-driven 单测：无版本、`/v1`、`/v1/`、带路径前缀、带 query、`/v1beta`（记录已知猜错面）、`http://127.0.0.1:11434/v1`、幂等性。验证：`go test ./internal/llm -run BaseURL`。

## 2. 接入写回与档案归一 [高优先级]

- [x] 2.1 给 `AgentAdapter` 增加协议族字段并在 5 个适配器上标注；`Switch` 在写配置前按族转换 `entry.BaseURL`，`SwitchOutput` 带出实际形态。验证：`go test ./internal/llm -run Switch`。
- [x] 2.2 `AddProvider` 落库前归一接入地址，发生改写时经既有 `Warnings` 提示；拒绝 userinfo 等既有校验语义不变。验证：`go test ./internal/llm ./cmd -run 'Provider|Add'`。
- [x] 2.3 补切换集成测试：档案接入地址缺 `/v1` 时，claude-code 配置写成无版本形态、codex 写成带版本形态，重复切换结果一致。验证：`go test ./internal/llm -run Switch`。

## 3. 可见性 [中优先级]

- [x] 3.1 `ai switch` 成功输出新增实际写入的接入地址一行。验证：`go test ./cmd -run Switch` 与 `go run . ai switch --help`。

## 4. 文档与整体验证

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md` 的 `ai switch` 说明：接入地址按 agent 协议族写回。验证：人工比对 skill 与 `go run . ai switch --help`。
- [x] 4.2 运行 `make check`，修复 fmt/vet/lint/race 问题。验证：`make check` 全部通过。
