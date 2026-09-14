# Design: tui-missing-mgmt-entries

## Context

三条 CLI 能力（`host unexport` / `keypair prune` / `mcp import`）的 TUI 入口被两处历史约定推迟：ssh-tui-apply-export 的 Non-goals（「CLI 先行，需求再现再补」）与 SKILL.md 的「`import` …仍走 CLI」。本 change 废止这两条约定，按仓库「产品形态」规则补齐入口。

既有模式（复用，不新造）：

- 写操作一律 `tea.Cmd` 异步执行，结果经自定义 msg 回主循环，成功 `okToast`、可恢复问题 `warnMsg`/`warnToast`、硬失败 `errMsg`（`model.go:117-124`），并 `recordAudit` 记本机操作审计（`internal/tui/audit.go`，nil writer 时 no-op）
- 确认 modal 走「mode 开关 + `updateMode` 分发 + `cancelMode` 清场」，如 `sshModeDeleteHost`/`kpModeMaterialize`；`esc/n` 取消、`enter/y` 确认是全仓库统一约定
- 键位单一真相源：`Bindings()` 注册 `KeyAction`，状态栏组名与 `?` 总览同源渲染（`keymap.go:5-13`），Update 分发与 Bindings 必须同步改，否则帮助漂移
- `cmd` 包 import `internal/tui`（`cmd/tui.go:20`），反向 import 成环——TUI 复用 import 逻辑必须先把纯函数从 `cmd/mcp_import.go` 下沉到 `internal/mcp`

能力审计给出的既有编排（纯消费，不改语义）：

- unexport：`Manager.Unexport()`（`internal/ssh/apply.go:101`）返回 `(unregistered, groupsRemoved, err)`，幂等，只动 Include 行与 `groups/`
- prune：`Manager.PruneCandidates()`（`internal/ssh/prune.go:23`）返回 `[]PruneCandidate{Path, Group, Name, InVault}`；`ssh.DeletePrunedFiles(paths)`（`prune.go:104`）逐删、汇总错误
- import：`readMCPImportFile` + `buildMCPImportEntry`（`cmd/mcp_import.go:123-193`），传输探测（显式 type > url→http > command→stdio）、模板原样、冲突不覆盖

## Goals / Non-Goals

**Goals:**

- 三个 Tab 各补一个管理入口，键位 mnemonic、与跨 Tab 动词表一致（`u` = unexport/revert、`i` = import、`p` = prune）
- 危险操作（prune 删私钥文件、unexport 删片段）保留 CLI 的「先列后确认」双步语义，确认内容与实际执行同一白名单
- import 逻辑下沉后 CLI/TUI 同源，CLI 输出逐字不变

**Non-Goals:**

- TUI import 的 dry-run（proposal 已列）；任何 CLI 语义/输出变化；持久化状态台账
- `internal/ssh` 的 `Unexport()`/`PruneCandidates()`/`DeletePrunedFiles()` 行为变化（只新增只读预检）

## Decisions

### D1 键位选择与占用证据

| Tab | 键位 | 动作 | 占用证据 |
|---|---|---|---|
| SSH | `u` | unexport | `ssh_tab.go:408-484` `updateKey` 现有键：↑/k ↓/j ←/h →/l `/` `ctrl+r` `enter` `n` `space` `a` `x` `d` `e` `g` `G` `pgup` `pgdown`——无 `u`；`Bindings()`（`ssh_tab.go:117-137`）亦无 |
| KeyPair | `p` | prune | `keypair_tab.go:358-415` `updateKey` 现有键：↑/k ↓/j ←/h →/l `/` `ctrl+r` `enter` `n` `i` `r` `e` `d` `m` `g` `G` `pgup` `pgdown`——无 `p`（注意 `i` 已被「import keypair」占用，`keypair_tab.go:385-388`） |
| MCP | `i` | import | `mcp_tab.go:376-462` `updateKey` 现有键：↑/k ↓/j ←/h →/l `/` `enter` `n` `e` `space` `a` `d` `x` `X` `u` `U` `g` `G` `pgup` `pgdown`——无 `i`（`u`/`U` 已被 revert 占用，`mcp_tab.go:448-451`） |

全局键（顶层 model）：`1-9` `tab/shift+tab` `S` `ctrl+r` `?` `esc` `q/ctrl+c`（`model.go:391-430`），与 `u`/`p`/`i` 无冲突；过滤态按键被 `handleFilterKeys` 截获，表单态按键进 form，新键只在 normal mode 生效。

语义先例：

- MCP Tab `u`/`U` = revert（unexport）已建立跨 Tab「u = 撤回导出」的动词先例；SSH `u` 对齐
- `keymap.go:70` 共享动词表已有 `actImport = {["i"], "import", grpItem}`，且 Text/Config Tab 的 `i` 均 = import（`text_tab.go:387`、`config_tab.go:496`）；MCP `i` 对齐共享动词表
- `p` = prune 首字母，KeyPair Tab 无冲突；各 Tab 键位独立作用域，他 Tab 的 `p` 不影响

**作用域决策（host 栏/侧栏都可用，不单点）**：`u` 与 `p` 在两个焦点栏均可用、不设 focus guard。理由：二者是 Tab 级全局操作，不读游标/多选集；既有全局操作 `ctrl+r`（刷新）、`/`（过滤）、`g`/`G`/`pgup`/`pgdown` （翻页跳顶底）在两栏都可用，而 `x`/`d`/`e`/`n` 限定 host/list 栏是因为它们作用于条目。备选（host 栏单点，与 `x` 对称）被否：unexport/prune 不消费焦点状态，guard 无安全收益，反而制造「为什么在侧栏按了没反应」的困惑。`Bindings()` 在两栏分支都注册，保证状态栏/`?` 总览与行为同源。

### D2 unexport：预检 + 确认框

`Unexport()` 只回执行后结果，确认框需要执行前状态（注册行是否在位、`groups/` 片段数）。新增只读预检：

```go
// internal/ssh（apply.go 或同包新文件 unexport_state.go）
func UnexportState() (registered bool, fragments int, err error)
```

实现复用既有规则：读 `~/.ssh/config` 逐行精确匹配 `IncludeLine`（与 `UnregisterInclude` 同一 `hasExactLine` 判定）；`groupsDir()` 下数 `*.conf`（与 `pruneGhostFragments` 同一后缀规则）。返回 error 仅在家目录解析/读取硬失败时。配对单测覆盖：无 config、有注册行无片段、有片段无注册行、两者都有。

确认框文案按预检结果逐项出现（无则缺省，全无为「nothing to unexport」直接 toast 不进 modal）：

```
unexport senv ssh export?
  - remove senv Include line from ~/.ssh/config        (仅 registered)
  - delete N group fragment(s) under ~/.ssh/senv/groups/  (仅 fragments>0, N 实填)
  - materialized private keys under ~/.ssh/senv/keys/ are kept
enter/y confirm · esc/n cancel
```

TOCTOU 说明：预检到确认执行之间用户另开 CLI 改动 ssh config 的概率忽略（单用户工具）；`Unexport` 本身幂等，最差结果是 toast 报告实际结果（如「nothing to unexport」），无破坏性错配。

### D3 prune：列表 + 确认同屏 modal

按 `p` → `tea.Cmd` 调 `PruneCandidates()` → 结果入 `kpPruneMsg`：

- `err` → `errMsg`
- 空清单 → `okToast("no unreferenced materialized keys")`，不进 modal
- 非空 → `kpModePrune` modal：逐行 `  <path>`，命中 `InVault` 追加 ` (keypair still in vault)`（逐字沿用 CLI 标注，`cmd/ssh.go:531-537`）；底部 `enter/y delete N file(s) · esc/n cancel`。单屏「先列后确认」，与 CLI 先打印清单再问 `delete N file(s)? [y/N]` 同序同语义，也复用 keypair 删除确认「列表 + 确认同屏」的既有交互（kpModeDeleteKey 列引用者）。

确认后 `tea.Cmd` 调 `ssh.DeletePrunedFiles(paths)`：返回 `(deleted, err)`，`err == nil` → `okToast("deleted N file(s)")` + 列表 reload；`err != nil`（部分删除失败， errs 汇总）→ `warnMsg` 含 `deleted X of Y: <首条错误>`，失败审计照常。审计 `recordAudit(mgrs, AuditOpSSHKey, "keypair:prune", ok, "prune N 个文件")`。

### D4 mcp import：逻辑下沉 + 路径表单 + 结果报告

**下沉**（任务先行，无下沉则 TUI 无法复用且成环）：`readMCPImportFile`、`buildMCPImportEntry`、`stringMap`、`stringSlice` 从 `cmd/mcp_import.go` 移入 `internal/mcp/import.go`，导出为 `ParseImportFile(path) (map[string]map[string]any, error)` 与 `BuildImportEntry(alias string, raw map[string]any) (*storage.MCPServerEntry, error)`。`cmd/mcp_import.go` 保留 flag、打印与审计，改为调 `internal/mcp` 同名逻辑——CLI 输出逐字不变，`cmd/mcp_import_test.go` 不改也应全绿（重构的验收标准）。

**表单**：`i` → 单字段 `formPath` 表单（label `agent config file`，placeholder `~/.claude.json | ~/.codex/config.toml`，validate 非空），提交时 `expandHome`。esc 取消走 `formCancelMsg` 既有路径（warnToast "cancelled"）。

**执行与结果报告**：提交 → `tea.Cmd`：`ParseImportFile` → 逐条目 `mgr.Get` 判冲突 → 非冲突 `BuildImportEntry` + `mgr.Add`，产出结构化报告 → `mcpImportDoneMsg{items []ImportResultItem, created, conflicts, failures int}`。条目解析/建档失败不中止其余（与 CLI 同）。

结果报告用 modal（`mcpModeImportReport`）而非仅 toast：

```
import report (~/.claude.json)
  github    create    stdio
  web       conflict  already exists
  bad-entry failed    entry has neither url nor command
created 1 · conflict 1 · failed 1        (仅 failed>0 时行尾附 “see CLI for details”)
esc/enter close
```

- 选 modal 不选 toast-only：冲突/失败的别名是用户下一步（去重命名或去 CLI dry-run）所需信息，toast 3 秒消失留不住；modal 复用既有 mode+`updateMode` chrome，成本一行 switch case
- 报告 MUST NOT 含值：只列别名、传输、动作与原因（原因来自 build 错误，本身无值）；列表 reload + 摘要 toast 同发（`mcpReloadMsg{toast: "imported: N created, M conflict, K failed"}`），`failures>0` 时 toast 级别 warn
- 审计：`recordAudit(mgrs, AuditOpMCPServer, "mcp:import", failures == 0, "import N 项 (...)")`，与 CLI 同 detail 格式

**TUI 不提供 dry-run**（Non-goal）：结果报告已呈现实际冲突跳过集合。

### D5 异步、审计与 help 同步

三入口执行体都在 `tea.Cmd` 内跑（同 `doBatchDeleteHosts`/`executeExport` 模式），期间 UI 可继续操作，结果经 msg 回环刷新；审计在 cmd 内 best-effort 记录（nil writer no-op）。每个入口同步注册 `Bindings()`：`u unexport`（SSH，两栏）、`p prune`（KeyPair，两栏）、`i import`（MCP，normal 组），`?` 总览随 Bindings 自动一致。

## 数据流图

### unexport（SSH Tab `u`）

```
用户按 u (normal mode)
  │ tea.KeyMsg "u"
  ▼
sshTab.updateKey ──► tea.Cmd: ssh.UnexportState()
  ▼ (async)                    sshUnexportStateMsg{registered, fragments, err}
err ──► errMsg                 无注册行且 0 片段 ──► okToast("nothing to unexport")
                               否则 mode = sshModeUnexport，渲染确认框(实填 N)
                                 │ enter/y ──► tea.Cmd: mgr.Unexport()
                                 │               ├─ recordAudit(op_ssh_host, "host:unexport", ok, ...)
                                 │               ▼ sshUnexportDoneMsg{unregistered, groupsRemoved}
                                 │                 toast 实报 + cancelMode
                                 └─ esc/n ──► cancelMode + warnToast("cancelled")
```

### prune（KeyPair Tab `p`）

```
用户按 p (normal mode)
  ▼
tea.Cmd: mgr.PruneCandidates()
  ▼ kpPruneMsg{candidates, err}
err ──► errMsg              空 ──► okToast("no unreferenced materialized keys")
                            非空 ──► mode = kpModePrune：列表(路径 + InVault 标注) + 底部确认行
                              │ enter/y ──► tea.Cmd: ssh.DeletePrunedFiles(paths)
                              │             ├─ recordAudit(op_ssh_keypair, "keypair:prune", ok, "prune N 个文件")
                              │             ▼ kpPruneDoneMsg{deleted, err}
                              │               全删 ──► okToast("deleted N file(s)") + reload
                              │               部分 ──► warnMsg("prune: deleted X of Y: 首条错误") + reload
                              └─ esc/n ──► cancelMode（零文件删除）
```

### import（MCP Tab `i`）

```
用户按 i (normal mode)
  ▼
enterImportForm：formPath 单字段表单（esc → formCancelMsg → warnToast "cancelled"）
  │ enter 提交 (expandHome)
  ▼
tea.Cmd: mcp.ParseImportFile(path)
  ├─ 读文件/解析失败 ──► mcpImportDoneMsg{err} ──► errMsg（vault 零变更）
  ▼ 逐条目：mgr.Get 判冲突 → BuildImportEntry → mgr.Add（失败不中止其余）
  ▼ mcpImportDoneMsg{items, created, conflicts, failures}
mode = mcpModeImportReport：逐条报告 + 计数，esc/enter 关闭
recordAudit(op_mcp_server, "mcp:import", failures==0, ...) + mcpReloadMsg{toast 摘要, warn: failures>0}
```

## 错误处理策略

| 失败点 | 处理 | 用户可见 |
|---|---|---|
| `UnexportState` 读 ssh config / home 失败 | `errMsg`，不进 modal，零副作用 | 底栏红字错误 |
| 预检后无可撤回项 | 直接 `okToast("nothing to unexport")` | toast |
| `Unexport()` 执行失败（两项各自独立，部分失败汇总，见 `apply.go:119-122`） | `errMsg` + 失败审计；已发生的单项（如注册行已摘）不回滚，与 CLI 同语义 | 底栏红字 + 审计 |
| `PruneCandidates` 失败 | `errMsg`，零副作用 | 底栏红字 |
| `DeletePrunedFiles` 部分失败 | `(deleted, err)` 都带回：已删不回滚，`warnMsg` 报 `deleted X of Y` + 首条错误 + 失败审计 | 黄条警告 + 列表 reload 反映实删 |
| import 文件不存在/解析失败/无条目 | `errMsg`（`ParseImportFile` 包装 `%w`），vault 零变更 | 底栏红字 |
| import 单条目 build/add 失败 | 该条标 `failed: <原因>`，继续其余；`failures>0` 时整体 toast warn + 审计 success=false | 报告 modal + 黄 toast |
| 确认 modal 中按其他键 | 忽略（同 grill D7 确认页收紧语义） | 无 |
| 异步 cmd 执行期间 | UI 不阻塞；结果经 msg 回环，与既有写操作一致 | — |

取消路径（`esc`/`n`/表单 `esc`）全部零副作用，`cancelMode` 清场后回 normal mode。

## Risks / Trade-offs

- 预检与实际执行之间的极小 TOCTOU 窗口（单用户工具，且 `Unexport` 幂等）→ 接受；toast 以实际结果为准
- `i` 在 MCP Tab 是新增动词，与共享动词表 `actImport` 对齐但 MCP 现有 `Bindings()` 用手写 `KeyAction`（不用共享常量）→ 沿用该文件风格手写一行，desc 用 `import`
- 结果报告 modal 增加一个 mode → 成本一行 `updateMode` case + 一个渲染分支；换来冲突别名留存，值不回显
- import 下沉动 `cmd` 包 → 以「`cmd/mcp_import_test.go` 零修改全绿」作重构验收，行为漂移即失败

## Migration Plan

无数据/格式迁移。发布时 SKILL.md 的 SSH/KeyPair/MCP 键位说明随本 change 更新；CLI 无变化，旧脚本不受影响。回滚 = revert 本 change，TUI 入口消失、CLI 与 `internal/mcp` 下沉函数保留亦无碍（CLI 继续消费）。

## Open Questions

无。TUI dry-run、持久化状态台账已在 Non-goals 明确排除，不改变 spec 与任务拆分。
