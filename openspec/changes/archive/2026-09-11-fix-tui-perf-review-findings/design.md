## Context

四个缺陷相互独立，但都落在 tui-perf 分支新引入的代码上：

1. **darwin 构建损坏**：`internal/securefs/stat_mtime_darwin.go:8` 使用 `stat.Mtimespec`，而 `golang.org/x/sys v0.41.0` 的 darwin `Stat_t` 字段为 `Mtim`（`Atim/Ctim` 同理）。`GOOS=darwin go build ./...` 在 securefs 包编译失败。（审查报告原指向 `internal/session/runtimefs_darwin.go`，经验证该文件使用的 `Statfs_t.Fstypename` 在此版本为 `[16]byte`，可正常编译——真实位置以本设计为准。）
2. **元数据清空丢失**：`EditProvider`（`internal/llm/provider.go:348`）以「字段指针/map 非 nil」表示「本次提供了该字段」，`assembleModels` 以 `opts.BaseMetadata = existing.ModelInfo` 播种旧值后仅叠加 override。因此「用户清空字段」只能表达为 nil（= 未提供），旧值经 BaseMetadata 回填幸存。TUI 侧（`internal/tui/ai_tab.go:841-860`）已正确算出 `xxxChanged`，但提交的 nil map/nil 指针无法传达清空意图；`parseDefaultReasoningField("")` 返回 `(nil, "", nil)` 也是同一表达的缺失。
3. **逃生舱静默胜出**：`loadCache`/`selectNewerCache`（`internal/session/cache.go:530-577`）在双缓存选新、以及安全存储读取失败回退两个分支直接返回磁盘逃生舱缓存，无警告无审计。写路径已有 `InsecureCacheWarning` 常量与 `insecureCacheEnabled` 进程标志（`internal/session/store.go`），读路径未复用。
4. **esc 语义**：`mcpTab.updateMode` 的 `mcpModeChangedConfirm` 分支（`internal/tui/mcp_tab.go:355-361`）把任何非 `y` 键（含 `esc`）记为「跳过该条」并继续，最终仍执行 `executeUnexport`；帮助文案承诺 `esc 取消`。同函数（774-792）缺少 `executeExport` 已有的 `plan.NeedsWrite()` 守卫，absent-only 计划也报「已撤回」并记成功审计。

## Goals / Non-Goals

**Goals:**

- `GOOS=darwin go build ./...` 与 `go vet` 全绿；darwin/linux 平台探针行为不变
- 编辑入口（TUI 表单与 CLI）显式清空元数据字段 → 档案中该维度条目被移除，且校验语义与 `add` 一致
- 读路径选中磁盘逃生舱缓存时：stderr 警告（进程级一次）+ cmd 层可记审计
- 逐条确认阶段 `esc` 取消整个撤回；absent-only 撤回不虚报成功
- 全部既有测试保持通过，新增针对性测试覆盖以上行为

**Non-Goals:**

- 不改 P2/P3 其余发现（错误信息脱敏、perflog 加固、`collectMu` 竞争窗口、rekey manifest 缓存锁不变式等）
- 不改缓存「选新者胜」的确定性选择策略本身（ADR-0017 已定），只补可见性
- 不为「清空」引入新的 CLI 交互（只让既有空值传参生效）；不做交互式逐字段确认
- 不重构 `assembleModels` 的合并优先级（显式 per-model > 档案已有 > 集合级 > 目录）

## Decisions

### D1. darwin mtime 修复：`Mtimespec` → `Mtim`

`stat_mtime_darwin.go` 改为 `stat.Mtim.Sec / stat.Mtim.Nsec`。这是 x/sys 的字段命名差异（darwin 用 `*tim`，freebsd/netbsd 用 `*timespec`），无行为变化。linux 文件不动。备选：升级/降级 x/sys 使 `Mtimespec` 存在——否决，x/sys darwin 从未有过 `Mtimespec`，属于源码笔误而非版本回归。

### D2. 清空哨兵：空非 nil map = 清空该维度；`DefaultReasoning` 沿用 `*string` 区分

`assembleModels` 内每个元数据维度（outputs/modalities/contexts/reasoning/default-reasoning）的归并改为两段式：

1. **播种**：维度 map 为 nil（未提供）或非空（增量覆盖）时，照旧从 `BaseMetadata`/目录播种；**空非 nil map（清空哨兵）** 时在目录合并之后、override 之前重置该维度（清掉档案与目录播种值），随后叠加该维度的显式 override（此时为空）。
2. **override**：现有 per-model 循环不变，仍校验模型必须在最终模型集内。

非空 map 保持增量覆盖语义不变——`add --model-context custom-1=x` 不得清掉目录里其他模型的 context（`TestAIProviderFullLifecycle` 锁定该行为）。

`DefaultReasoning`（集合级）已是 `*string`：nil = 未提供，非 nil（含 `""`）= 参与；配合「default-reasoning 维度是否清空」的新表达决定旧 per-model 值是否回填。`EditProviderOptions` 的触发条件（provider.go:348）保持「任一字段非 nil 即重排」，无需改动——清空意图由空非 nil map / 非 nil 指针自然携带。

TUI 侧：`doSubmitProvider` 在 `xxxChanged` 时按字段传 `llm.ClearingMap`（nil → 空非 nil），未变更字段不进 opts（避免空哨兵误清未展示的目录元数据）。共享的 `llm.Parse*` 对空输入仍返回 nil，语义不变——清空哨兵只在「已显式提供」的调用点注入。CLI 侧：`cmd/ai_provider.go` 在 flag `Changed` 时同样经 `ClearingMap` 赋值，`--model-output ""` 即清空；未传 flag 行为不变。

备选：为每个字段增加显式 `ClearXxx bool`——否决，字段已 10 个，成对布尔让调用面翻倍且易漏；空集合哨兵在 Go 惯用（`json`/`merge-patch` 风格）且 diff 清晰。备选：TUI 绕过 provider 层直接改 `entry.ModelInfo`——否决，绕过校验与审计。

### D3. 逃生舱选中可见性：读路径复用 `InsecureCacheWarning` + 进程内选中状态

`internal/session` 新增包级 `sync.Once` + 原子标志：`loadCache`/`selectNewerCache` 返回逃生舱缓存的两个分支（胜出、回退）调用 `markHatchSelected()` → 首次向 stderr `Fprintln` 既有 `InsecureCacheWarning`，并置 `hatchSelected` 原子标志。cmd 层在 session 相关命令收尾（与现有审计埋点同处）读取该标志，为 true 时记一条审计事件（如 `op_session` + detail `cache-source=disk-hatch`，不含密钥）。TUI 同样在进入时读标志记审计。

备选：让 `Load` 返回 `(cache, source, err)` 三元组把来源贯穿到 cmd——否决，调用链上 `Load` 有多处、改签名波及面大；进程级标志对本需求（告警一次 + 审计痕迹）已充分。备选：只告警不审计——否决，spec 要求两者。

### D4. esc 取消与 NeedsWrite 守卫：对齐 executeExport 既有模式

`mcpModeChangedConfirm` 分支头部加 `if key == "esc" { t.cancelMode(); return t, warnToast("已取消撤回") }`；帮助文案（该模式渲染处）已在承诺 `esc 取消`，无需改文案。`executeUnexport` 在 goroutine 开头加 `if !plan.NeedsWrite() { 提示「无需写入」；return }`，与 `executeExport`/`cmd/mcp_export.go` 的守卫一致，审计仅在真正执行时记录。顺带消除该分支对 `t.changedItems()` 的重复调用（一次取列表复用 idx 与长度判断）。

## Risks / Trade-offs

- [空非 nil map 哨兵被既有调用方误传] → 触发条件是「显式构造空非 nil map」，现有 CLI/TUI 路径在「未提供」时全部传 nil（grep 验证）；新增测试锁定「nil map 不清空」与「空 map 清空」两个方向。
- [清空 context window 后必填校验失败] → 这是预期行为（与 `add` 一致报错、不留部分更新），TUI 表单经统一提示条回显；设计不放宽校验。
- [集合级 `DefaultReasoning=&""` 与「清空维度」组合语义] → 规则收敛为一条：default-reasoning 维度被显式提供（per-model map 或集合级指针任一非 nil）时，旧 per-model 默认档不回填，随后按「显式 per-model > 集合级 > 目录」重新填充；测试覆盖「清空后目录默认档重新接管」场景。
- [stderr 警告在非 TTY/捕获输出场景噪音] → 与写路径警告同策略：仅 stderr、进程级至多一次；eval 捕获的是 stdout，不受影响。
- [esc 分支吞掉逐条确认中已答 `y` 的意图] → spec 明确「整体取消，含已答 y 的条目不删除」，与计划页 esc 行为一致；实现即 `cancelMode()`，不持久化部分结果。

## Migration Plan

纯代码修复，无数据迁移、无配置项变化。合入后 `GOOS=darwin go build ./...` 恢复即可发版；回滚 = revert 对应提交。缓存/档案数据格式不变。

## Open Questions

（无——审查发现即为需求输入，实现路径均已在代码中验证。）
