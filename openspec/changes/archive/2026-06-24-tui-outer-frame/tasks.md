## 1. 样式层重构（styles.go）

- [x] 1.1 重命名 `titleBarStyle` → `paneTitleStyle`，注释改为「栏内 pane 的小标题（如 "Groups (n)"、列表 header）」，全量替换 `env_tab.go`/`text_tab.go`/`config_tab.go` 共 6 处引用。**验证**：`go build ./internal/tui/...` 通过，`rg titleBarStyle internal/tui/` 无残留。
- [x] 1.2 在 `styles.go` 新增 `frameStyle`（`RoundedBorder()` + 运行时设 `Width/Height`、Padding(0,0)）与 `tabStripStyle`（底线用 `Border(NormalBorder,false,false,true,false)` + `BorderForeground(muted)` + Padding(0,1)，因 `BorderBottom(true)` 在 v1.1.0 单独不生效），重做 `tabStyle`（Foreground muted，无背景）与 `activeTabStyle`（`Background(accent)` + `Foreground(white)` + Bold），另加 `tabSeparatorStyle`。**验证**：编译通过，样式定义有注释说明用途。

## 2. 尺寸计算（model.go Update）

- [x] 2.1 修改 `WindowSizeMsg` 分支：计算 `contentW = m.width - 2`、`contentH = m.height - 7`（外框 2 + tab strip 含底线 2 + status bar 1 + tab pane 自身边框 2），下发给 `tabs[*].SetSize(contentW, contentH)`。（原任务写 `contentH = m.height - 4`，但 lipgloss v1.x border 在 Width/Height 之外，tab pane 会多渲染 2 行边框，故调整为 -7。）保留注释并更新数学说明。**验证**：80×24 下三 tab 均渲染恰好 24 行。

## 3. 渲染层（model.go View）

- [x] 3.1 在 `View()` 顶部、`m.height == 0` 守卫之后，新增最小尺寸守卫：`if m.height < 7 || m.width < 30` 返回居中提示 `"terminal too small (need ≥30×7)"`，不进入正常渲染。（阈值从 `<6` 调整为 `<7`：tab strip 底线在 lipgloss v1.x 下占独立一行，需多预留 1 行。）**验证**：手动设 m.width=20 调用 View() 返回提示串，不含 box-drawing 字符。
- [x] 3.2 重做 tabStrip 拼装：active 用 `activeTabStyle.Render(label)`、其余用 `tabStyle.Render(label)`，tab 之间用 `tabSeparatorStyle.Render("│")` 插入两个分隔符，整体用 `tabStripStyle.Width(contentW).Render(...)` 包裹并加底线。（`BorderBottom(true)` 在 lipgloss v1.1.0 不生效，改用 `Border(NormalBorder,false,false,true,false)` 实现 tab strip 底线。）**验证**：View() 输出含 3 个 label 与 2 个 `│`，active label 被 background ANSI 序列包裹。
- [x] 3.3 用 `frameStyle.Width(m.width - 2).Height(m.height - 2).Render(...)` 包裹 `JoinVertical(tabStrip, content, bottom)` 的输出并返回。（lipgloss v1.x border 在 Width/Height 之外，故传 m.width-2 使总宽 = m.width。）确保 frame 固定铺满终端。**验证**：输出首行以 `╭` 开头、`╮` 结尾；末行以 `╰`/`╯` 收尾；行宽等于 m.width。
- [x] 3.4 调整错误条截断：使用 `truncateRunes(m.err, contentW-4)`（contentW = m.width-2，再减去 `⚠ ` 前缀与 bar padding），并硬截断避免 lipgloss Width 的 word-wrap 增行。**验证**：长错误信息不溢出右竖边框。

## 4. 测试（与实现配对）

- [x] 4.1 审查并更新现有受影响测试（`model_test.go`、`env_tab_test.go`、`text_tab_integration_test.go`、`config_tab_integration_test.go`）中所有精确字符串匹配：优先改为 `strings.Contains` 或调整期望以容纳外框/分隔符。**验证**：`go test ./internal/tui/...` 不因字符串失配而 fail（功能断言保持）。（现有测试已用 `contains` + 直接调用 `tab.View()`，全绿，无需改动。）
- [x] 4.2 新增 `TestOuterFramePresent`：构造 80×24 的 Model，调用 `View()`，断言首行含 `╭`、末行含 `╰`、每行长度 ≤ 80。**验证**：`go test ./internal/tui/ -run TestOuterFramePresent -v` 通过。
- [x] 4.3 新增 `TestActiveTabHasBackgroundBlock`：默认 active=0（Env），断言 `View()` 输出中 "Env" 子串被 lipgloss background ANSI 转义（`48;5;` 属性）包裹，"Text"/"Config" 不被背景色包裹。测试中用 `lipgloss.DefaultRenderer().SetColorProfile(termenv.ANSI256)` 强制着色。**验证**：`go test ./internal/tui/ -run TestActiveTabHasBackground -v` 通过；切换 active=1 后断言反转。
- [x] 4.4 新增 `TestTabSeparatorsPresent`：断言 `View()` 输出中 tab 栏行内 `│` 恰好出现 2 次（在 Env 与 Config 之间）。**验证**：`go test ./internal/tui/ -run TestTabSeparators -v` 通过。
- [x] 4.5 新增 `TestMinSizeGuard`：设 m.width=20/m.height=10 与 m.width=80/m.height=5 两种情况，断言 `View()` 返回 "terminal too small" 提示且不含 box-drawing 字符。**验证**：`go test ./internal/tui/ -run TestMinSizeGuard -v` 通过。
- [x] 4.6 新增 `TestContentDoesNotOverflowFrame`：构造 80×24，断言 `View()` 每行 rune 数 ≤ 80，行数恰好 24，无行跨越外框。**验证**：`go test ./internal/tui/ -run TestContentDoesNotOverflow -v` 通过。

## 5. 收尾与验收

- [x] 5.1 运行 `make check`（fmt + vet + lint + test）全绿，`go test -race ./internal/tui/...` 通过。**验证**：无报错，无 lint 警告。（golangci-lint 未安装，go vet 已通过；`make check` 输出 "All checks passed!"。）
- [ ] 5.2 手动 TTY 验收：`./senv tui` 在正常尺寸（80×24）确认外框、tab 色块、分隔符、底部状态栏均在框内；缩窗至 30×6 边界确认布局完整；缩至 20×10 确认显示最小尺寸提示。切换 Env/Text/Config 三 tab 内容不溢出。**验证**：四个尺寸场景视觉符合 specs/tui-viewer/spec.md 的每个 Scenario。
- [ ] 5.3 回归确认：`senv tui` 的搜索 overlay（`S`）、modal 编辑（`e`/`n`）、vim 衔接（`e` 在 text/config）仍正常工作，外框在 overlay 弹出/退出时不残留错位。**验证**：手动触发 overlay 与 modal，进出后外框完整。
