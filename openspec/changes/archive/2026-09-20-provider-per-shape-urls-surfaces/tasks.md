# Tasks

## 1. TUI

- [x] 1.1 AI Tab provider 表单（新建/编辑）新增三个形态地址字段：编辑预填、清空提交 = 显式清除、非法值内联报错；字段说明提示 anthropic 地址填 root（不带 `/v1`）；补 TUI 测试

## 2. MCP

- [x] 2.1 `cmd/mcp_llm.go`：`llmProviderView` 新增 `api_shape`（omitempty）与 `shape_urls`（`{chat?, responses?, anthropic?}`，omitempty）；补 MCP 测试（含未声明时键省略）；`go run . mcp list-tools` 核对

## 3. 文档与 ADR

- [x] 3.1 `.agents/skills/senv-cli/SKILL.md` 回写：provider add/edit `--shape-url`、switch 门禁放宽与输出来源、anthropic 地址原样存储约定、TUI 表单字段、MCP 视图字段
- [x] 3.2 晋升仓库 `docs/adr/0027-provider-per-shape-urls.md`（标注与 ADR-0004/0006 的修订关系；0004/0006 Status 互链 0027）

## 4. 验证

- [x] 4.1 `go test ./...` 全绿；`golangci-lint run --new-from-rev=origin/main` 新代码 0 issue；`go run . --help`、`go run . ai provider --help`、`go run . mcp list-tools` 语法核对；`openspec validate --strict --type change provider-per-shape-urls-surfaces` 通过
