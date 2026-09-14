# Proposal: tui-missing-mgmt-entries

## Why

按仓库 AGENTS.md「产品形态」规则做能力审计，发现三条已有 CLI 能力的 TUI 入口缺失：`senv host unexport`、`senv keypair prune`、`senv mcp import` 只能走 CLI。这并非疏漏而是两处历史遗留的显式推迟：ssh-tui-apply-export 的 Non-goals 写着「TUI 的 `host unexport` / `keypair prune` 入口（CLI 先行，需求再现再补）」，SKILL.md 仍写着 MCP Tab「`import` …仍走 CLI」。现在按约补齐，废止这两条推迟约定。

## What Changes

- **SSH Tab 新增 `u`（unexport）键**：异步预检后弹确认框，列出「将移除 `~/.ssh/config` 的 senv 注册行 + 删除 N 个组片段；`~/.ssh/senv/keys/` 落盘私钥保留」；`enter/y` 确认后异步执行 `Manager.Unexport`（与 CLI 同一编排），结果 toast 呈现
- **KeyPair Tab 新增 `p`（prune）键**：先弹候选列表（路径 + `(keypair still in vault)` 标注，与 CLI 输出一致）再确认；确认后调 `PruneCandidates`/`DeletePrunedFiles` 同一白名单删除，vault 永不动
- **MCP Tab 新增 `i`（import）键**：路径表单（`~` 展开）提交后异步导入，弹出结果报告（逐条 create/conflict/failed + 新建/冲突跳过/失败计数）；import 解析与传输识别逻辑从 `cmd/mcp_import.go` 下沉到 `internal/mcp`，CLI 行为与输出不变
- 三个入口都走 `tea.Cmd` 异步 + toast + 本机操作审计（target 只含标识不含值），与仓库既有写操作模式一致

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `ssh-assets`: 新增「TUI 导出撤回与私钥清理」requirement，覆盖 SSH Tab `u` unexport 与 KeyPair Tab `p` prune 两个入口
- `mcp-server-import`: 「导入命令与输入」requirement 扩展 TUI `i` 入口（路径表单、与 CLI 同语义建档、执行后结果报告）

## Impact

- `internal/tui/ssh_tab.go`（`u` 键、`sshModeUnexport` modal、keyactions/help）；`internal/tui/keypair_tab.go`（`p` 键、`kpModePrune` 列表+确认 modal）；`internal/tui/mcp_tab.go`（`i` 键、路径表单、`mcpModeImportReport` 结果报告）
- `internal/ssh`：新增 `UnexportState` 预检（注册行是否在位 + `groups/` 片段计数），纯只读
- `internal/mcp`：新增 import 解析/建档纯逻辑（自 `cmd` 下沉）；`cmd/mcp_import.go` 变薄包装，既有 `cmd/mcp_import_test.go` 保持绿
- `.agents/skills/senv-cli/SKILL.md`：SSH/KeyPair/MCP 三个 Tab 的键位说明同步，删除「import 仍走 CLI」约定

## Non-goals

- TUI 的 mcp import `--dry-run`：CLI 保留 `--dry-run`，TUI 以结果报告呈现实际建档/冲突跳过结果
- 改动 CLI 侧 unexport/prune/import 的语义、输出与确认流（本 change 是纯消费方 + 下沉式重构）
- 新建持久化「导出/清理状态」台账：确认框与结果报告即状态呈现，与 ssh-host-export-apply 的取向一致
- vault 数据模型、落盘路径约定（ADR-0023）与同步行为的任何变化

## 安全性分析

本 change 涉及外部状态（`~/.ssh/config`、`~/.ssh/senv/` 目录树）与私钥文件删除，三个入口都保留 CLI 的「先列后确认」双步语义，无一步直达删除的键位：prune 的删除集合仍由 `PruneCandidates` 白名单决定（仅未被任何 host 引用的落盘文件，`InVault` 仅作标注不改变删除范围），vault 档案 MUST NOT 出现在删除路径上；unexport 只摘除 senv 精确拥有的 Include 行并删除 `groups/` 目录，写 ssh config 前留 `.senv-bak` 的规约不变，`keys/` 与 vault 明确保留并写进确认框文案。import 下沉不改解密面：值仍按模板原样入库（`{{env:...}}` 不解析），TUI 结果报告只含别名、传输与计数，不含值。三入口执行后均记本机操作审计（unexport→`op_ssh_host`、prune→`op_ssh_keypair`、import→`op_mcp_server`），失败同样记审计。
