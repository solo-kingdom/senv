# Tasks: tui-missing-mgmt-entries

## 1. internal/ssh：UnexportState 只读预检（高优·安全）

- [x] 1.1 新增 `ssh.UnexportState() (registered bool, fragments int, err error)`：复用 `hasExactLine` 判定 `IncludeLine` 在位、`groupsDir()` 下数 `*.conf`（design D2）；纯只读，不改 `Unexport()` 任何行为
- [x] 1.2 1.1 配对单测（internal/ssh，临时 HOME）：覆盖无 config、注册行在位无片段、有片段无注册行、两者皆有、config 不可读五种情形（验证：`go test ./internal/ssh/ -race` 全绿）

## 2. SSH Tab：unexport 入口（高优·安全）

- [x] 2.1 `u` 键接入：normal mode 两栏可用（design D1 作用域决策），异步预检 → 无可撤回项 toast 直达；否则 `sshModeUnexport` 确认框逐项列出「移除注册行（仅在位）/删除 N 个组片段（仅 >0）/落盘私钥保留」；`enter/y` 异步 `mgr.Unexport()` + 审计 + 结果 toast，`esc/n` 取消零副作用；`Bindings()` 两栏注册 `u unexport`
- [x] 2.2 2.1 配对单测（internal/tui，临时 HOME）：已应用导出态按 `u` → 确认框文案含片段数与「keys kept」→ `y` 后注册行与片段删除、keys/ 与 vault 原样；无可撤回项 toast 且零文件副作用；`esc` 取消零副作用（验证：`go test ./internal/tui/ -race -run SSH` 全绿）

## 3. KeyPair Tab：prune 入口（高优·安全）

- [x] 3.1 `p` 键接入：normal mode 两栏可用；异步 `PruneCandidates()` → 空清单 toast 直达；非空进 `kpModePrune` 列表+确认同屏 modal（路径 + `(keypair still in vault)` 标注，逐字对齐 CLI）；`enter/y` 异步 `ssh.DeletePrunedFiles` + 审计，全删 toast、部分失败 warnMsg 不回滚；`esc/n` 零删除；`Bindings()` 两栏注册 `p prune`
- [x] 3.2 3.1 配对单测（internal/tui，临时 HOME）：构造被引用 + 未引用（含 InVault）落盘文件，按 `p` 列表断言标注与计数，`y` 后仅未引用文件被删、vault 不变、`esc` 路径零删除；部分失败（只读文件）断言 warn 与已删不回滚（验证：`go test ./internal/tui/ -race -run KeyPair` 全绿）

## 4. mcp import：逻辑下沉 + MCP Tab 入口

- [x] 4.1 下沉重构：`readMCPImportFile`/`buildMCPImportEntry`/`stringMap`/`stringSlice` 移入 `internal/mcp/import.go`，导出 `ParseImportFile` 与 `BuildImportEntry`；`cmd/mcp_import.go` 改为薄包装（flag/打印/审计不变）；CLI 输出逐字不变——`cmd/mcp_import_test.go` 零修改全绿作为验收（验证：`go test ./cmd/ -race` 全绿）
- [x] 4.2 MCP Tab `i` 键：normal mode 弹单字段 `formPath` 表单（`~` 展开、非空校验）；提交后异步 `ParseImportFile` + 逐条目 `Get` 判冲突/`BuildImportEntry` + `Add`（失败不中止），产出 `mcpImportDoneMsg`；`mcpModeImportReport` modal 逐条列 create/conflict/failed 及原因 + 计数（不含值），esc/enter 关闭；摘要 toast（failures>0 为 warn）+ 列表 reload + 审计；`Bindings()` 注册 `i import`
- [x] 4.3 4.2 配对单测（internal/tui + internal/mcp）：JSON/TOML 各导入一次断言报告条目与计数；已存在别名冲突跳过且现有档案不变；非法 JSON 报错且 vault 零变更；表单 esc 取消零副作用（验证：`go test ./internal/... -race` 全绿）

## 5. 文档与回归

- [x] 5.1 更新 `.agents/skills/senv-cli/SKILL.md`：SSH Tab 补 `u` unexport、KeyPair Tab 补 `p` prune、MCP Tab 键位行删除「`import` …仍走 CLI」并补 `i` 导入说明（验证：`go run . --help`、`go run . mcp import --help`、`go run . mcp list-tools` 正常；SKILL 所述按键与实现抽查一致）
- [x] 5.2 全量回归（验证：`make check` 全绿）
