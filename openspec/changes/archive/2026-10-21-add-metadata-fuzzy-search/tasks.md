# Tasks

## 1. 共享搜索核（高优先级：安全边界）

- [x] 1.1 新增 `internal/search`：`Item`、空 query 校验、token AND、`Contains` 匹配、exact/prefix/substring 排序与 limit；验证 `go test ./internal/search/ -count=1` 覆盖空 query、仅值不命中、description 命中、limit 截断
- [x] 1.2 实现全类型 Gather（env/text/backup/config/ssh/llm/mcp），结果结构无 value 字段；验证单元测试断言 JSON/结构体不含 value，且 mock/fixture 中仅存在于 value 的串不命中

## 2. CLI

- [x] 2.1 实现 `senv search`（`--kind` / `--limit` / `--json` / session / 可选 `--refresh`），注册到 root；验证 `go run . search --help` 与空 query 退出非 0
- [x] 2.2 CLI 集成测试：写入带 description 的 env/text 与 ssh 等后搜索；验证命中元数据、`--json` 无 `value`、`--kind` 过滤生效（`go test ./cmd/ -run Search -count=1` 或等价）

## 3. MCP

- [x] 3.1 注册 `senv_search` 并接入既有 guard；验证 `go run . mcp list-tools` 含该工具，工具数文档同步准备
- [x] 3.2 MCP 测试：有 session 命中、无 session 拒绝、空 query 错误、结果无 value；验证 `go test ./cmd/ -run MCPSearch -count=1`（或等价）通过

## 4. TUI 对齐

- [x] 4.1 全局搜索 gather/match 对齐 metadata-search 字段（含 env/text/config description 等）；验证 TUI 相关测试或新增 search 包字段表测试覆盖 description 命中
- [x] 4.2 确认空输入仍展示 inventory、有输入走共享匹配；验证既有 `internal/tui` 搜索相关测试通过

## 5. 文档与集成

- [x] 5.1 更新 `.agents/skills/senv-cli/SKILL.md`（及仓库内镜像若需）：MCP 工具数、`senv search` / `senv_search` 用法与「结果无值」；验证 skill 描述与 `mcp list-tools` 一致
- [x] 5.2 跑 `make test`（或项目惯用测试命令）相关包无回归；验证搜索路径不改变 `senv_env_list` 既有带 value 行为（既有 MCP env list 测试仍绿）

## Workflow follow-up

- Archive the change after review / apply complete.
- 可选：发版后同步本机 `senv` 二进制。
