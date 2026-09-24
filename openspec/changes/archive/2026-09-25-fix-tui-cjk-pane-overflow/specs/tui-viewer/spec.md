# tui-viewer Delta

## MODIFIED Requirements

### Requirement: 面板内容截断与详情

TUI 的所有面板内容 MUST 不依赖 lipgloss `Width` 换行：列表行与详情行 SHALL 在面板宽度内截断（超长以 `…` 结尾），完整内容 SHALL 通过 `enter` 打开的详情弹层查看。任何面板 MUST NOT 因长值（base_url、模型列表、hostname、路径）而撑高或折行。截断与列对齐 SHALL 按显示列（display width）计算——CJK 全角字符 1 rune = 2 列，MUST NOT 按 rune 数截断。详情弹层 SHALL 占用与被它顶替的面板相同的位置（同宽、同高，含边框），且在滚动或内容不足一页时 MUST NOT 改变尺寸——滚动时窗口边缘固定不动。

任何渲染路径产出超过终端高度预算的视图时，MUST 在终端行数内硬裁（保留顶部 Tab 栏可见）：顶部 Tab 栏 MUST NOT 因任一面板内容（含 CJK 键名、描述、值、分组名）而被挤出屏幕。

#### Scenario: 长值截断
- **WHEN** provider 的模型列表或 base_url 超过所在面板宽度
- **THEN** 该行在面板内截断显示，面板高度与行数不变

#### Scenario: 详情弹层看全文
- **WHEN** 用户在列表上按 `enter`
- **THEN** 弹出详情层展示未截断的完整字段，`esc` 关闭并回到列表

#### Scenario: 详情弹层几何稳定
- **WHEN** 用户打开详情弹层并在其中上下滚动，或所看内容不足一页
- **THEN** 弹层的四边位置不变（与打开前该面板的外框重合），只有正文行内容随滚动变化

#### Scenario: CJK 内容不撑高面板
- **WHEN** 条目键名、描述、值或分组名包含中文（CJK 全角字符），且数据量超过单屏
- **THEN** 列表仍按窗口化滚动展示，视图总高度不超过面板预算，顶部 Tab 栏保持可见

#### Scenario: 渲染超高兜底
- **WHEN** 任一渲染路径（含未来新增路径）产出超过终端行数的视图
- **THEN** 输出在终端行数内硬裁，顶部 Tab 栏 MUST NOT 被挤出屏幕
