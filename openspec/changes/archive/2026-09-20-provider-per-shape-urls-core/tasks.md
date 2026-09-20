# Tasks

## 1. Schema 与校验

- [x] 1.1 `internal/storage/types.go`：`LLMProviderEntry` 新增 `ChatBaseURL` / `ResponsesBaseURL` / `AnthropicBaseURL`（JSON `chat_base_url` / `responses_base_url` / `anthropic_base_url`，omitempty）；`ValidateLLMProvider` 扩展三字段 URL 校验（同 `BaseURL`：HTTPS 默认、拒绝 userinfo/空 host；空值合法）；补 storage 层测试（存量档案不含新字段可读取）

## 2. 归一化

- [x] 2.1 `internal/llm/baseurl.go`：新增 Anthropic 形态归一（仅收敛尾斜杠 + 既有 URL 合法性校验，不动路径段）；OpenAI 族形态字段复用现归一；`internal/llm/provider.go` 写入路径按 key 分派归一函数；补单测（`/v1` 输入原样、`/api/anthropic` 前缀不受影响、尾斜杠收敛、OpenAI 族补版本段）

## 3. switch 门禁与地址解析

- [x] 3.1 `internal/llm/switch.go`：门禁放宽（目标族显式地址放行，替换现形态校验分支）与三动作拒绝文案；地址解析（claude-code：`anthropic_base_url` 原样 → `BaseURL` 推断；OpenAI 族：线协议对应字段 → `BaseURL`）；成功输出含实际写入地址与来源（explicit 字段名 / 由 BaseURL 推断）；补单测并回归「未设形态地址时行为与现状一致」

## 4. CLI

- [x] 4.1 `cmd/ai_provider.go`：add/edit 新增重复 flag `--shape-url <api_shape>=<url>`（key 校验、设置/空值清空/edit 省略保留）；`show`/`list` 展示形态地址（沿用 `orDash` 风格）；`go run . ai provider add --help` / `edit --help` 语法核对；补 CLI 测试

## 5. 验证

- [x] 5.1 `go build ./...`、`go test ./...` 全绿；`golangci-lint run --new-from-rev=origin/main` 新代码 0 issue；`openspec validate --strict --type change provider-per-shape-urls-core` 通过
