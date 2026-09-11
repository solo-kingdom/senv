## Why

代码审查（`docs/reviews/2026-09-11/senv-tui-perf/review.md`）在 tui-perf 分支发现 1 个 P0 与 6 个 P1 缺陷，集中在三条主线：macOS 构建损坏（发布阻断）、LLM 档案元数据「清空不生效」（本分支元数据增强功能的核心语义缺陷）、session 磁盘逃生舱静默胜出与 MCP Tab 撤回确认的 esc 语义（安全/交互缺陷）。这些缺陷已在 main 合入路径上，应在下一次发布前修复。

审查更正：审查报告将 P0 归因于 `internal/session/runtimefs_darwin.go`（`Fstypename` 类型），经规划期验证该说法不成立——本仓 `x/sys v0.41.0` 中 darwin `Statfs_t.Fstypename` 为 `[16]byte`，该文件可编译。真实 P0 是 `internal/securefs/stat_mtime_darwin.go` 使用 `stat.Mtimespec`，而 darwin `Stat_t` 只有 `Mtim` 字段，`GOOS=darwin go build ./...` 实际失败。本提案按验证后的真实位置修复，验收以 darwin 交叉编译通过为准。

## What Changes

- 修复 darwin 构建损坏：`internal/securefs/stat_mtime_darwin.go` 改用 darwin `Stat_t.Mtim`，恢复 `GOOS=darwin go build ./...`（P0）
- LLM 档案元数据清空语义：`EditProvider`/`assembleModels` 增加「显式清空」表达（空非 nil map 作为清空哨兵），`assembleModels` 对清空的元数据维度先重置再应用（不再被 `BaseMetadata` 旧值回填）；TUI 表单与 CLI 编辑入口在字段被清空时传递清空哨兵。覆盖 default_reasoning、outputs、modalities、contexts、reasoning 五个维度（P1）
- session 磁盘逃生舱选择可见性：当读路径选中磁盘逃生舱缓存（胜过可读的安全存储缓存，或安全存储不可用），MUST 以进程级 once 向 stderr 输出既有 `InsecureCacheWarning`，并提供查询接口供 cmd 层记入操作审计（P1）
- MCP Tab 撤回逐条确认的 esc 语义：`mcpModeChangedConfirm` 分支显式处理 `esc`——取消整个撤回、不删除剩余条目、提示已取消，与帮助文案一致（P1）

范围界定：仅覆盖上述 P0/P1（及其同根因的 P1 重复项），外加一个例外：审查 #68「撤回无待写入条目时虚报成功」（P2）——它与 esc 修复位于同一需求（`mcp-server-tui` 导出与撤回）与同一函数（`executeUnexport`），顺带修复避免二次触碰同一 spec。其余 P2/P3 发现不在本提案内，留待后续提案。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider`: 「编辑 LLM Provider 档案」——编辑入口显式清空元数据字段时 MUST 生效（删除对应元数据），不得静默保留旧值
- `llm-provider-tui`: 「AI Tab 档案写操作」——编辑表单清空元数据字段并提交后，档案对应元数据被移除并在详情中消失
- `session-auth`: 「session 缓存仅驻留平台安全存储」——读路径选中磁盘逃生舱缓存时 SHALL 输出安全警告并留审计痕迹
- `mcp-server-tui`: 「导出与撤回」——逐条确认阶段按 `esc` SHALL 取消整个撤回操作，剩余条目不删除

## Impact

- `internal/securefs/stat_mtime_darwin.go`：darwin 专属编译修复（`Mtimespec` → `Mtim`），无行为变化
- `internal/llm/provider.go`：`EditProviderOptions` / `AddProviderOptions` 语义扩展（空非 nil map = 清空该维度）；`assembleModels` 维度重置逻辑；`cmd/ai_provider.go` 与 `internal/tui/ai_tab.go` 的清空传参
- `internal/session/cache.go` / `store.go`：逃生舱选中告警（进程级 once）与选中状态暴露；`cmd` 层 session 相关命令记审计
- `internal/tui/mcp_tab.go`：`updateMode` 逐条确认分支的 esc 处理
- 兼容性：清空哨兵仅由「编辑入口显式传空非 nil map」触发；既有调用方传 nil 行为不变（非破坏）。darwin 修复无接口变化
- 验证：`GOOS=darwin go build ./...`、`go test ./...`，以及元数据清空/逃生舱告警/esc 取消的针对性测试
