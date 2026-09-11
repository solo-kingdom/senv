## 1. 存储层：字段与校验

- [x] 1.1 `MCPServerEntry` 增加 `URL`/`Headers` 字段（omitempty），`ValidateMCPServer` 按传输分支：stdio 要求 command 且禁 url/headers；http/sse 要求 url 以 http(s) 开头且禁 command/args/env；header 键为合法 HTTP token。验证：`go test ./internal/storage/ -run MCPServer` 三传输校验矩阵全绿。
- [x] 1.2 【高优先级/安全】为 1.1 补校验测试：模板 url（含 `{{env:...}}`）通过、非法 scheme 拒绝、字段越界拒绝、新旧条目互读回归（旧 stdio 密文可读）。验证：新增用例先红后绿，`make test ./internal/storage/`。

## 2. agentcfg：写入形状与渲染

- [x] 2.1 `Server` 增加 `Transport`/`URL`/`Headers`，指纹规范化对新增字段 omitempty（旧 stdio 指纹不变）；`serverFromMap` 解析 `type`/`url`/`headers`。验证：指纹回归测试（旧条目指纹不变）+ 读侧识别用例。
- [x] 2.2 JSON 族渲染按传输分支：remote 写 `type`/`url`/`headers`（headers 空则省略），不写 command/args/env；保留其它键与其它 server。验证：渲染保真测试（含非空 headers、空 headers、混合配置）。
- [x] 2.3 TOML 族：remote 渲染 `url` 表键；新增 `TOMLServers` 枚举原语（供 import）。验证：TOML 渲染与枚举测试（既有表内容保留、`.bak` 不丢）。
- [x] 2.4 核验 7-agent 能力矩阵（design 表）：逐 agent 对照官方文档确认路径、键名、http/sse 支持与 headers 键（重点：codex headers、kimi 新路径 `~/.kimi-code/mcp.json`、pi 适配器形态、zcode 实机 `~/.zcode/cli/config.json`+`mcp.servers`）；结论落为 `agentcfg` 内的能力声明与（如证实偏差）注册表路径/键修正。验证：每个 (agent, transport) 组合有「支持+键集」或「不支持」的结论记录，注册表修正有对应测试。
- [x] 2.5 【高优先级/安全】能力矩阵单元测试：不支持的 (agent, transport) 渲染必须报错而非产出键名拼凑的配置。验证：矩阵全组合用例覆盖。

## 3. 导出/撤回链路

- [x] 3.1 `resolveEntry` 按传输解析模板：remote 解析 `url` 与 header 值（严格模式），不解析 env。验证：单测覆盖解析成功/失败隔离（该 agent 报错、文件不动）。
- [x] 3.2 导出计划：目标不支持该传输时产出 `error` 条目并写明原因；明文标注覆盖 url/headers。验证：claude-desktop remote 导出用例（error、文件字节不变、其余 agent 继续）。
- [x] 3.3 端到端导出：remote 档案 → JSON 族与 TOML 族目标写入，指纹/漂移三态/`--force`/skip 语义与 stdio 一致。验证：`cmd/mcp_export_test.go` 扩展用例全绿。
- [x] 3.4 unexport 适配 remote 条目：读侧指纹比对、内容一致直接删、被改需确认。验证：remote unexport 用例（一致删/漂移确认/其它条目保留）。

## 4. CLI：add/edit/get/list/import

- [x] 4.1 `add` 增加 `--transport`（stdio|http|sse）、`--url`、`--header`（可重复 `Name: Value`），校验字段组合；`edit` 增加 `--transport`/`--url`/`--header`/`--unset-header`，切换传输校验失败不落库。验证：`go run . mcp add --help` 与各参数组合的手工/集成用例。
- [x] 4.2 `get` 输出 url/headers；`list` remote 行显示 `scheme://host` 来源，不输出 query 与 header 名/值。验证：【高优先级/安全】泄漏测试——list 输出断言不含 query 值与 header 值。
- [x] 4.3 新增 `senv mcp import <file> [--dry-run]`：解析 JSON `mcpServers` 族与 Codex TOML，分类规则（显式 type > url→http > command→stdio > 失败继续），冲突跳过不覆盖，逐条报告 + 汇总。验证：JSON/Codex TOML 夹具集成用例（含冲突、无法识别条目、`--dry-run` 不落库）。
- [x] 4.4 import 审计与错误路径：文件不存在/解析失败整体报错不落库，审计记录成功/失败。验证：审计与退出码用例。

## 5. TUI 与 MCP 工具面

- [x] 5.1 MCP Tab 表单：新建/编辑支持传输选择（stdio/http/sse），remote 显示 url（必填）与 headers（`$EDITOR` 按 `Name: Value` 行编辑），隐藏 command/args/env；编辑允许切换传输并按目标传输校验。验证：表单提交/内联报错用例（缺 url、缺 command、切换传输）。
- [x] 5.2 【高优先级/安全】TUI 值可见性：列表/详情/计划页/toast 不渲染 header 值与 url query（remote 显示 `scheme://host` 与 header 键名）；`$EDITOR` 是 headers 唯一解密面。验证：渲染断言用例（含带 query url + Bearer header 档案）。
- [x] 5.3 左栏列表与详情展示 remote 信息（传输、来源 host）；导出计划页 error 条目（不支持传输）可读展示。验证：`mcp_tab_test.go` 扩展用例。
- [x] 5.4 `mcp_server_list` 视图确认：remote 条目 alias/transport 正常返回，url/headers 值不出现。验证：`mcp_guard`/视图测试断言响应字段。

## 6. 文档与收尾

- [x] 6.1 更新 `.agents/skills/senv-cli/SKILL.md`：新增 `--transport`/`--url`/`--header`、`senv mcp import`、remote 导出能力矩阵语义。验证：`go run . --help`、`go run . mcp add --help`、`go run . mcp import --help`、`go run . mcp list-tools` 与文档一致。
- [x] 6.2 全量回归：`make check`（fmt + vet + lint + test -race）。验证：CI 本地全绿。
