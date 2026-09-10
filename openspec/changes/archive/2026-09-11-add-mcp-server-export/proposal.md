## Why

senv 目前只能把「senv 自己」的 MCP server 写进 agent 配置（`senv mcp install`）。用户自有的 MCP server 定义只能散落在 7 个 Coding Agent 的配置文件里各写一遍，改一次要改七处、极易分叉。senv 已是 env/text/config 的唯一事实源，MCP server 定义应当同样由 vault 保管、导出到各 agent 的全局配置。

## What Changes

- 新增 MCP Server 档案数据类型：加密存 vault，别名唯一标识，V1 仅 stdio（`command`/`args`/`env`）；值支持既有 `{{env:...}}`/`{{text:...}}` 引用
- 新增 `senv mcp add/list/remove`：档案 CRUD
- 新增 `senv mcp export`：按目标 agent 的配置格式（JSON `mcpServers` / TOML `[mcp_servers.*]`）合并写入其**全局配置**；必须显式给目标（`--agent a,b` / `--all`），先出计划再执行
- 新增 `senv mcp unexport`：撤回导出，语义对齐 `config uninstall`（内容一致才删，被改过需确认）
- 本机导出台账 `~/.config/senv/mcp-exports.json`（agent → 别名 → 内容指纹），指纹不符即判为漂移，默认拒绝覆盖，`--force` 放行
- 新增只读 MCP 工具 `mcp_server_list`（仅 alias / 传输类型 / 描述，不含值）
- `.agents/skills/senv-cli/SKILL.md` 1.2 → 1.3（`AGENTS.md` 硬要求）

## Non-goals

- 不支持 `http`/`sse` 传输（延后；各 agent 键名不统一且无权威公开文档）
- 不透传 agent 特有键（`disabled`、`autoApprove` 等）
- 不新增 project 级导出（沿用 `--scope` 仅 cursor 生效的现状）
- 不回读 agent 配置作为事实源，不做双向同步
- 不提供写侧 MCP 工具，不把 senv 自身 server 建模成内置档案
- 不引入凭据代理转发（沿用 ADR-0002 的边界）

## 安全性分析

档案值（`env` 常为 token）加密存 vault；但 `export` 会把解析后的明文写入目标 agent 配置文件（0600 + 备份）。理由是 agent 无法从 vault 拉取凭据，agent 配置是它唯一的读取路径——与 ADR-0002 同一取舍，见 ADR-0008。缓解：计划页逐条标注明文条目与目标路径，确认后才落盘；`--dry-run`/`--print` 不写盘。同机同账户边界内的其它进程仍可读该文件，这一层不由 senv 负责。

## Capabilities

### New Capabilities

- `mcp-server`: MCP Server 档案的加密存储与管理（add/list/remove）
- `mcp-server-export`: 档案导出到多 agent 全局配置、撤回与漂移处理
- `mcp-server-mcp`: MCP 只读查询 MCP Server 档案

### Modified Capabilities

（无）

## Impact

- 新增内部包（档案 manager + 导出器）；复用 `cmd/mcp_agents.go` 的 agent 注册表与 `cmd/mcp_install.go` 的 JSON/TOML merge 原语（需下沉供两处共用）
- vault 存储新增条目类型：仅新增，现有 vault 无需迁移（旧库读不到该类型即视作空）
- `cmd/mcp*.go`、`internal/storage`、`.agents/skills/senv-cli/SKILL.md`
- 已落文档：`CONTEXT.md` 词条、`docs/adr/0007`、`docs/adr/0008`

## 验证记录

- 2026-09-11：`make check`（fmt + vet + lint + go test -race ./...）全部通过。
- 2026-09-11：`go run . mcp --help`（10 个子命令）、`go run . mcp export --help`、`go run . mcp add --help` 与实际 flag 一致；`go run . mcp list-tools` 输出 21 个工具，含只读 `mcp_server_list`。
- 2026-09-11：`openspec validate add-mcp-server-export --strict` 通过。
- 回归面：`internal/storage` 新增 `mcp_servers` 集合已登记进 `rekey.go`、`rekey_manifest.go`（漏登记会 panic）、`consistency.go`（含 `HasOrphanedData`）、`ssh.go` 的 `entryKindForDir`、`cmd/doctor.go`；rekey 后档案可读、一致性检查覆盖该目录均有测试。
