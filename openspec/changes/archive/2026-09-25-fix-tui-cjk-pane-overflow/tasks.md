# Tasks

- [x] 1. 复现并定位根因（rune 口径截断 + lipgloss Height 不裁剪 + 常态路径无最终 clipLines；30 个中文键名渲染 31 行 vs 预算 17 行）
- [x] 2. 宽度层：Text/Backup 条目行、renderSidebar（六 Tab 侧栏）、Env 值预算（改 lipgloss.Width）、Config formatConfigItemLine/formatPlanLine、History 预览、四个 loadErr 早退路径，统一 truncateWidth/padRight 显示列口径
- [x] 3. 高度层：stackWithOverlay 无 overlay 分支补 clipLines(paneBudget)；model View() frame 输出后 clipLines(framed, m.height) 最终硬裁
- [x] 4. 回归测试：TestTextTabCJKKeysStayWithinPaneBudget、TestSidebarCJKGroupNamesStayWithinPaneBudget
- [x] 5. 验证：go build ./... && go vet ./internal/tui/ && go test ./... 全绿
