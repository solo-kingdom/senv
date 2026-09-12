## Why

TUI 8 个 Tab 的加载行为与面板几何三种形态并存：AI/MCP/SSH 数据装载期间显示带操作指引的空态文案（如「暂无 LLM Provider 档案；…按 n 新建」）而非加载提示；history/audit 面板边框随内容伸缩（实测装载后 62×6 vs 其他 Tab 78×19）、resize 不跟随、加载态是无框裸文本。与 env/text/config 的既有范式（常驻面板 + 框内「加载中…」）不一致，属 tui-ux 收尾后遗留的一致性欠账。

## What Changes

- AI/MCP/SSH Tab 补齐加载态：`!t.loaded` 时在常驻双栏几何内嵌「加载中…」提示，空态文案仅在装载完成后出现（grill D3）
- History/Audit 面板改为撑满内容区（`Width×Height`），列表接入共享 `windowedPane`（窗口化标题 + 防溢出 clip），加载/空/错误三态内嵌面板，resize 跟随重排（grill D4）
- spec 层固化：`tui-viewer` 新增「Tab 加载态」与「History 与 Audit 面板几何」两条需求（grill D2 delta 策略）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: 新增「Tab 加载态」（跨 Tab：装载期间常驻几何 + 框内提示，空态指引只允许装载完成后出现）与「History 与 Audit 面板几何」（撑满内容区、windowedPane 窗口化、resize 跟随）两条需求；既有需求零 MODIFIED

## Impact

- 代码：`internal/tui/ai_tab.go`、`mcp_tab.go`、`ssh_tab.go`（加载守卫）、`history_tab.go`、`audit_tab.go`（几何重排）及各自测试
- 文档：`openspec/specs/tui-viewer/spec.md`（归档时合并）

## Non-goals

- 操作过程进度提示（MCP 导出/撤回、AI 切换、history 恢复）——另立任务（grill D2）
- history/audit 双栏化重设计、spinner/骨架屏
- 按键语义与数据装载路径变更（懒加载门控、单趟快照语义不变）

## 验证记录
