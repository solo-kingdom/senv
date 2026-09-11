## Why

分组浏览有两种范式并存：config 是「侧栏 + All 伪组 + 过滤感知计数」，env/text 还是旧式左侧组列表（无 All、无计数）。grill D3 已定：config 范式推广为标准，env/text 升级同款，不给 SSH/AI/MCP 加数据分组。

## What Changes

- env/text 左栏升级为 config 同款侧栏：All 伪组置顶（默认选中）、其下分组列表带条目计数、计数随 `/` 过滤更新；All 视图右侧显示全部条目（行前缀 `group/key`）
- env 保留 default 分组置顶与 `(default)` 标识、`●` 激活标记、组级 `t`/重命名/删除操作；text 空分组统一显示（计数 0，与 config 过滤期行为一致）
- `←→/hl` 双栏焦点切换语义与 config 一致（config-tui spec 已约束，env/text 对齐）
- 全局搜索跳转定位（group+item）在侧栏结构下继续生效

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: 「Env Tab 浏览与操作」「Text Tab 浏览与操作」升级为侧栏范式（基于 keymap 之后的基线：组激活/停用 `t`、text 导出 `x`）

## Impact

- 代码：`internal/tui/env_tab.go`、`text_tab.go`、列表组件侧栏渲染泛化（config 侧栏实现下沉共享）
- 文档：`.agents/skills/senv-cli/SKILL.md`

## Non-goals

- 不给 SSH/AI/MCP 加数据分组；不改 config 侧栏行为（它是范式源）；不动多选/过滤语义

## 验证记录
- 2026-09-11（分支 tui-ux）：config 侧栏范式下沉为共享 `renderSidebar`（SidebarRow），config/env/text 三处消费，config 行为等价；env/text 装载层追加 All 伪组（置顶、默认选中、计数=条目总数）并让条目携带 group 字段；All 视图聚合全部分组条目（group/key 前缀、组名排序）；多选/选择标识改为 it.group 前缀（跨组安全）；All 上组操作（t/r/d）护栏提示；新建/导入落组走 realGroup（All 视图回落 default 或要求 group:key）；`→` 切栏定位第一条；text 空分组改显示（计数 0）；相关测试断言更新（All 置顶、fixture 落 default 组）；SKILL.md 侧栏段重写；`make check` 全部通过。
