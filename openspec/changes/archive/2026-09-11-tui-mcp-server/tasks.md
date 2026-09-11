## 1. Tab 注册与装配

- [x] 1.1 `tui.Managers` 增加 MCP 管理器与导出台账路径；`New` 在 AI 之后、History 之前注册 MCP Tab，nil 时跳过。验证：`go test ./internal/tui -run 'Tabs|New'`（全量注入时 Tab 数为 8，仅 env/text/config 时仍为 3）。
- [x] 1.2 `cmd/tui.go` 解锁后注入 `mcp.Manager`（对齐 SSH），Resolve 闭包复用 env/text 解引用；LedgerPath 指向 config dir。验证：`go test ./cmd -run TUI`（若无现成用例则补一条装配不报错的测试）且 `go run . tui --help` 仍可用。

## 2. 两栏浏览与详情

- [x] 2.1 左栏列出档案（alias / command / env 键数），右栏列出 `agentcfg.Supported()`，按当前档案的只读 `Plan` 显示未导出 / 已导出 / 漂移；`←→` 切栏、`↑↓` 只动焦点栏；长行截断。验证：`go test ./internal/tui -run MCP`。
- [x] 2.2 `enter` 打开档案详情：command、args、env 键名、引用模板原文；字面量不出现在渲染文本。空档案显示简体中文空态。验证：`go test ./internal/tui -run MCP` 含「详情/列表不含 env 值」断言。

## 3. 档案 CRUD [高优先级]

- [x] 3.1 新建/编辑表单：别名（新建可填、编辑只读）、command 必填、description；args/env 走 `formEditor`（一行一 arg、一行 `KEY=VALUE`）；transport 不出现、写死 stdio。主表单 env 只显示键名。验证：`go test ./internal/tui -run MCP`。
- [x] 3.2 提交调用 `Add`/`Update`：command 空或 alias 冲突时表单内联报错且不写；成功刷新左栏并记 `op_mcp_server`（不含值）。验证：`go test ./internal/tui -run MCP`。
- [x] 3.3 `d` 二次确认删除：台账已导出时确认文案列出 agent id 并说明不撤回；只调 `Delete`，agent 配置字节不变。验证：`go test ./internal/tui -run MCP`。

## 4. 导出与撤回 [高优先级]

- [x] 4.1 `x`/`X` 以当前档案 × 当前/全部 agent 调用 `Plan([]string{alias})`（禁止空 alias slice）；计划页列出动作、路径、`[明文 env]`，不渲染解析值；`y`/`enter` 执行、`esc`/`n` 取消不写盘。验证：`go test ./internal/tui -run MCP`。
- [x] 4.2 计划页 `F` 以 `Force=true` 重算 Plan 后再确认；漂移默认 skip。`u`/`U` 走 `PlanUnexport` + `ExecuteUnexport`，被改过的条目逐条 `y/n`。验证：`go test ./internal/tui -run MCP`。
- [x] 4.3 导出/撤回记 `op_mcp_export`（target 含 agent/alias，不含值）；部分失败 toast 汇总且其余继续。无选中档案按 x/u 只 toast。验证：`go test ./internal/tui -run MCP`。

## 5. 搜索与文档

- [x] 5.1 全局搜索纳入 MCP 档案 alias 与 command，不匹配 env 值；`enter` 跳到 MCP Tab 并 `focusJump`。验证：`go test ./internal/tui -run Search`。
- [x] 5.2 更新 `.agents/skills/senv-cli/SKILL.md` TUI 键位（MCP Tab）与 README TUI 章节。验证：对照 `?` 键位总览文案与 skill 正文一致。
- [x] 5.3 `make check` 全绿；`openspec validate tui-mcp-server --strict` 通过。验证：两条命令均成功。
