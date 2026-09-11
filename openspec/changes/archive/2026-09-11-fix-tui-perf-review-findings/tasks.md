## 1. P0：恢复 darwin 构建

- [x] 1.1 修改 `internal/securefs/stat_mtime_darwin.go`：`stat.Mtimespec` → `stat.Mtim`（Sec/Nsec 取值不变）
- [x] 1.2 验证 `GOOS=darwin go build ./...` 与 `GOOS=darwin go vet ./...` 通过，linux 侧测试不受影响

## 2. P1：LLM 档案元数据清空语义（llm-provider / llm-provider-tui）

- [x] 2.1 `internal/llm/provider.go` `assembleModels`：元数据归并改两段式——map 维度（outputs/modalities/contexts/reasoning/default-reasoning）在「空非 nil map = 显式清空」时不从 `BaseMetadata` 播种该维度，再叠加 override；集合级 `DefaultReasoning` 沿用 `*string` 语义，default-reasoning 维度被显式提供时旧 per-model 默认档不回填
- [x] 2.2 `internal/tui/ai_tab.go`：`parseDefaultReasoningField("")` 及各元数据解析函数对空输入返回空非 nil map；`doSubmitProvider` 在 `xxxChanged` 且解析结果为 nil 时传空非 nil map（保证 changed ⇒ 意图必达）
- [x] 2.3 `cmd/ai_provider.go`：`--model-outputs ""` 等显式空值产生空非 nil map；未传 flag 仍为 nil
- [x] 2.4 单测：`internal/llm/provider_test.go` 覆盖「nil map 不清空（回归）」「空 map 清空对应维度」「清空默认推理档后目录默认档可重新接管」「清空后必填校验失败不留部分更新」
- [x] 2.5 单测：`internal/tui/ai_tab_test.go` 覆盖编辑表单清空默认推理档/输出上限/输入模态提交后，档案与详情不再含旧值、重开表单字段为空
- [x] 2.6 `go run . ai provider edit --help` 与 `.agents/skills/senv-cli/SKILL.md`：如有空值清空语义的用户可见变化，同步技能文档

## 3. P1：磁盘逃生舱选中可见性（session-auth）

- [x] 3.1 `internal/session/cache.go`：新增包级 `sync.Once` + 原子标志；`selectNewerCache` 逃生舱胜出分支与 `loadCache` 安全存储失败回退分支调用标记函数，首次向 stderr 输出 `InsecureCacheWarning`
- [x] 3.2 `internal/session`：导出逃生舱选中状态查询（如 `HatchCacheSelected() bool`），cmd 层 session 相关命令收尾据此记审计（detail 含 `cache-source=disk-hatch`，不含密钥）
- [x] 3.3 单测：`internal/session/cache_test.go` 覆盖「双缓存逃生舱较新胜出 → 警告一次 + 标志置位」「安全存储失败回退 → 警告」「仅安全存储 → 无警告」「同进程第二次不重复警告」
- [x] 3.4 单测：cmd 审计路径覆盖逃生舱选中事件可查（复用既有 session 审计测试模式）

## 4. P1+P2：MCP Tab 撤回 esc 取消与 NeedsWrite 守卫（mcp-server-tui）

- [x] 4.1 `internal/tui/mcp_tab.go` `updateMode` 的 `mcpModeChangedConfirm` 分支：`esc` 显式 `cancelMode()` + 已取消 toast，不进入逐条跳过逻辑；顺带消除 `changedItems()` 双重调用（取一次列表复用）
- [x] 4.2 `internal/tui/mcp_tab.go` `executeUnexport`：加 `plan.NeedsWrite()` 守卫，absent-only 计划提示「无需写入」，不执行、不记成功审计（对齐 `executeExport`）
- [x] 4.3 单测：`internal/tui/mcp_tab_test.go` 覆盖「逐条确认中 esc → 全部条目不删除、台账不变、提示已取消」「absent-only 撤回 → 不显示已撤回、无成功审计」「y 确认路径回归不变」
- [x] 4.4 检查逐条确认帮助文案与实际按键行为一致（`esc 取消`），不一致处以实现为准修文案

## 5. 收尾验证

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` 全绿
- [x] 5.2 `GOOS=darwin go build ./...` 通过（P0 验收门）
- [x] 5.3 `go run . --help`、`go run . mcp list-tools` 抽查 CLI 行为无回归；对照 `openspec validate` 通过
- [x] 5.4 对照 `docs/reviews/2026-09-11/senv-tui-perf/review.md` 逐条复核 P0/P1（含 #21/#26/#40 同根因项）已修复或明确豁免
