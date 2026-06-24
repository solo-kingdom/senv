## Why

`senv tui` 的顶部 tab 栏当前只是一行漂浮文字（active 靠粗体粉色、inactive 靠灰色区分），没有容器、没有边框、tab 之间也没有分隔。手动验收时发现两个问题：（1）三个 tab 不像可切换的 tab，缺乏视觉暗示；（2）整个 TUI 顶部没有边框，看起来未完成。`styles.go` 里定义的 `titleBarStyle`（注释写明"tab strip 的容器"）从未被 `View()` 引用，是死代码——证明原本就计划做容器但没接上。

## What Changes

- 给整个 TUI 套一圈外边框（lipgloss `RoundedBorder`），形成「应用窗口」视觉边界
- tab 标签放进顶部边框行内（类似窗口标题栏），active tab 通过背景色块 + 下划线明显区分，tab 之间用 `│` 分隔
- 底部状态栏/错误栏纳入外框内部，与顶部对称
- 调整 `WindowSizeMsg` 的尺寸计算：content 区宽高各减去边框占用的 2 字符，避免内容溢出/错位
- 清理死代码：删除或正式启用 `titleBarStyle`
- 保持所有现有交互行为（Tab/数字键切换、快捷键、搜索 overlay、modal）完全不变，纯视觉层改动

## Non-goals

- **不改动** 任何 tab 的功能逻辑、数据加载、Manager 调用——本次仅触碰 `View()` 渲染层与尺寸计算
- **不重做** 配色方案（粉/紫/灰调色板保持，仅可能微调对比度）
- **不引入** 动画、焦点边框动画、tab 拖拽等新交互
- **不调整** 底部错误栏/状态栏的文本内容与触发逻辑，只改其容器样式
- **不重构** Tab 接口签名或 model 结构

## Capabilities

### New Capabilities
<!-- 无。本次是已有 tui-viewer 能力的视觉规约增强。 -->

### Modified Capabilities
- `tui-viewer`: 新增「视觉外框与 Tab 可辨识性」要求——TUI SHALL 以单一边框包裹整个界面，active tab MUST 在视觉上与 inactive tab 明显区分

## Impact

- **改动代码**：`internal/tui/model.go`（`View()` 拼装外框、`Update()` 的 `WindowSizeMsg` 尺寸数学）、`internal/tui/styles.go`（外框样式、重做 tab 样式、清理 `titleBarStyle`）
- **不改动**：`cmd/`、`internal/{env,text,config,session}/`、加密/存储层、依赖列表
- **测试**：现有渲染测试可能因外框字符变化需要更新断言；新增针对外框存在性与 active tab 视觉区分的单元测试
- **风险**：尺寸计算若出差错会导致内容溢出或滚动异常，需在窄宽/矮高（如 60×15）边界下验证

## Security Analysis

本变更纯属视觉呈现层，**不触及任何加密、密钥派生、存储、session 或敏感值遮蔽逻辑**。敏感值遮蔽策略（env 默认 `***`、text/config 列表只显元信息、搜索不匹配值）保持不变。外框与 tab 样式不引入新的数据展示路径，无新增攻击面。
