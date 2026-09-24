## 根因

两层叠加，缺一不可：

1. **宽度层**：`truncateRunes` 按 rune 数截断。CJK 全角字符 1 rune = 2 显示列，含中文的键名/描述/值实际行宽可达预算 2 倍，超出 pane 内容宽后被 lipgloss `Width` 折成两行。
2. **高度层**：窗口化组件（`windowedPane`/`clipLines`）只保证「逻辑行数」，折行发生在它们之后；且实测 **lipgloss `Height` 只补齐、不裁剪**（`Height(3).Render(5 行)` 输出仍是 5 行，frame 同理）。`stackWithOverlay` 只在有 overlay 时做最终 `clipLines`，常态路径没有兜底。bubbletea 全量打印 View，超高时终端只显示底部 → 顶部 Tab 栏被顶出屏幕。

## 方案

- **宽度层（正本清源）**：所有进入面板的行截断统一为显示列口径 `truncateWidth`（已有工具，标题/搜索/AI Tab 均在用），列对齐 `padRunes` 换 `padRight`。Env Tab 值的预算计算从 `len([]rune(keyLabel))` 改为 `lipgloss.Width(keyLabel)`。
- **高度层（纵深防御）**：`stackWithOverlay` 无 overlay 分支补 `clipLines(renderBody(innerH), paneBudget(innerH))`；model `View()` 在 frame 渲染后再 `clipLines(framed, m.height)`。即使未来新增渲染路径漏算，终端行数也被硬裁，顶部不再溢出。

## 关键实现

- `internal/tui/list.go`: `stackWithOverlay` 常态路径兜底；`renderSidebar` 改 `truncateWidth`
- `internal/tui/model.go`: frame 输出最终 `clipLines(framed, m.height)`
- `internal/tui/text_tab.go` / `backup_tab.go`: 条目行两处截断
- `internal/tui/env_tab.go`: 组描述 `truncateWidth(40)`；值截断预算改 `lipgloss.Width`
- `internal/tui/config_tab.go`: `formatConfigItemLine` / `formatPlanLine` 列对齐与截断
- `internal/tui/history_tab.go`: 预览截断口径统一
- `ai_tab.go` / `mcp_tab.go` / `keypair_tab.go` / `ssh_tab.go`: `loadErr` 早退路径改 `truncateWidth(maxInt(t.width-2, 8))`

## 回归测试

- `TestTextTabCJKKeysStayWithinPaneBudget`：30 个中文键名，视图高度 ≤ `paneBudget`
- `TestSidebarCJKGroupNamesStayWithinPaneBudget`：25 个中文分组名，侧栏视图高度 ≤ `paneBudget`

## 备选方案

- 只加最终 `clipLines` 兜底、不改截断口径：能防溢出但中文行被拦腰截断、显示畸形，且窗口化滚动语义仍错（一行占两行）。故宽度层必须修。
- 引入 bubbletea viewport 组件：当前窗口化实现已满足交互需求，引入组件是更大重构，非本次范围。
