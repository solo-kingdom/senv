## Why

Text Tab（以及 Backup、Config、Env、六个 Tab 的分组侧栏）在键名、描述或值为中文（CJK）且数据较多时，不是窗口化滚动，而是整个视图上移、顶部 Tab 栏被挤出屏幕。根因是行宽截断按 rune 数而非显示列宽（CJK 1 rune = 2 列），行被 lipgloss `Width` 折行撑高；而 lipgloss `Height` 实测只补齐不裁剪，常态渲染路径（`stackWithOverlay` 无 overlay 分支、model 最终输出）又缺最终行数兜底，超高视图被 bubbletea 全量打印后顶部溢出。复现：30 个中文键名，pane 预算 17 行，实际渲染 31 行。

## What Changes

- 所有渲染进面板的行截断从 rune 口径（`truncateRunes`/`padRunes`/`truncRunes`）统一改为显示列口径（`truncateWidth`/`padRight`）：Text/Backup 条目行、共享侧栏 `renderSidebar`（config/env/text/backup/keypair/ssh 六处）、Env Tab 值截断（预算改用 `lipgloss.Width(keyLabel)`）、Config Tab `formatConfigItemLine`/`formatPlanLine` 列对齐、History 预览、ai/mcp/keypair/ssh 四个 `loadErr` 早退路径
- `stackWithOverlay` 无 overlay 的常态路径补最终 `clipLines(paneBudget(innerH))` 兜底
- model `View()` frame 渲染输出后再按终端行数 `clipLines(framed, m.height)` 硬裁，作为最后一道防线
- 新增回归测试：CJK 键名条目视图不得超高、CJK 分组名侧栏不得超高

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: 强化「面板内容截断与详情」要求——截断以显示列（display width）为准（CJK 全角 1 rune = 2 列），且任何渲染路径超高时 MUST 在终端行数内硬裁，顶部 Tab 栏 MUST NOT 被挤出屏幕。

## Impact

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | `internal/tui`：list.go（stackWithOverlay/renderSidebar）、model.go（View 最终兜底）、text_tab.go、backup_tab.go、env_tab.go、config_tab.go、history_tab.go、ai_tab.go、mcp_tab.go、keypair_tab.go、ssh_tab.go、pane_window_test.go |

无 CLI 面变化（纯 TUI 渲染修复），无存储/协议变化。
