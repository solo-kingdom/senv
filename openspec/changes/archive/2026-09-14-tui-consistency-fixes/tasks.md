## 1. keymap 共享常量与 KeyPair（design D1/D2）

- [x] 1.1 keymap.go 按既有 `act*` 范式新增 `actSelect`（`space`，toggle select）与 `actSelectAll`（`a`，select all visible，grpItem）；keypair_tab.go 的 `kpModeDeleteKey` 确认态注册 `{F, force delete (clear host identityKey), grpConfirm}`（验证：`go build ./...` 通过；临时单测断言 keyPairTab 删除确认态 `Bindings()` 含 `F`、普通态含 `space`/`a` 之外的键位无回归）
- [x] 1.2 1.1 配对单测：keyPairTab 两种态的 `Bindings()` 键集合与描述（验证：`go test ./internal/tui/ -race -run KeyPair` 全绿）

## 2. 多选键逐 Tab 注册（design D1）

- [x] 2.1 env_tab.go / text_tab.go / config_tab.go 的 `Bindings()` 注册 `actSelect`、`actSelectAll`（验证：`go build ./...`；三 Tab 普通态 `Bindings()` 断言含 `space` 与 `a`）
- [x] 2.2 ssh_tab.go（host 栏分支）/ mcp_tab.go 的 `Bindings()` 注册 `actSelect`、`actSelectAll`（验证：`go build ./...`；SSH host 栏态与 MCP 普通态 `Bindings()` 断言含 `space` 与 `a`，侧栏态不含）
- [x] 2.3 2.1–2.2 配对单测：五 Tab 注册后 `?` 总览数据源的键集合（验证：`go test ./internal/tui/ -race -run Bindings` 全绿）

## 3. 帮助与分发一致性契约（design D3）

- [x] 3.1 新增 `keymap_consistency_test.go`：契约表逐 Tab 断言「Update 分发处理的键 ⊆ `Bindings()` 注册的键」，覆盖 KeyPair 删除确认态 `F` 与五 Tab 的 `space`/`a`（验证：`go test ./internal/tui/ -race -run Consistency` 全绿；临时移除 2.1 一处注册后该测试必须变红，随后恢复）
- [x] 3.2 3.1 配对清理：把 1.1/2.1/2.2 中临时断言并入契约表，删除一次性测试代码（验证：`go test ./internal/tui/ -race` 全绿且无重复用例）

## 4. README 勘误与回归（design D4）

- [x] 4.1 README.md 快捷键表 4 处改写：激活/停用 env 组 `a`→`t`（env_tab.go:432）、导出 text `o`→`x`（text_tab.go:391）、AI 换默认模型 `m`→`M`（ai_tab.go:435）、刷新 `r`→`ctrl+r` 并括注条目栏 `r`=重命名（keymap.go:71）（验证：逐行 diff 核对 4 处；与 SKILL.md 对应描述抽查一致，SKILL.md 键位语义不改）
- [x] 4.2 全量回归（验证：`go test ./internal/tui/ -race` 全绿；`go run . --help` 与 `go run . tui --help` 正常输出；契约表覆盖的 6 个键在帮助渲染路径 `newHelpTab` 数据源中出现）
