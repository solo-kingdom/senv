## Why

SSH Tab 的 Host/KeyPair 全部按名字典序平铺，Host 数量稍多即难以定位；`HostEntry.Tags` 字段早已存在却完全未用于列表组织。Env/Text/Config 三个 Tab 均已有「分组侧栏」范式，SSH/AI/MCP 三个双栏 Tab 缺失该维度，交互不一致。本 change 为 SSH 数据补充分组展示，并顺修 KeyPair 栏的过滤与关联可见性问题（AI/MCP Tab 的分组另开 change 复用范式）。

## What Changes

- `HostEntry` 新增单值 `group` 字段（空 = 未分组）；`--group` 贯通 `senv ssh host add/edit` 与 TUI 表单
- Tags 保持多值标注正交于 group：Host 列表行行尾展示 `#tag`（最多 2 个 + `+n`，窄屏截断），参与 `/` 过滤
- SSH Tab 改为三栏「分组侧栏 → 组内 Host → KeyPair」，复用 Env/Text/Config 的 `renderSidebar` 范式：All 伪组置顶、组名 (n) 字母序、「未分组」置底（无未归类 Host 时不出现）
- KeyPair 栏：新增 `/` 过滤；行内引用计数 `被 N 个 Host 引用`，零引用灰显「未被引用」，排序保持名字序
- `/` 过滤匹配 alias + hostname + tags + group；`S` 全局搜索纳入 group + tags

## Non-goals

- AI/MCP Tab 分组（后续 change）
- 按 ProxyJump 拓扑组织展示
- KeyPair 自身的分组字段
- Host 分组作用于导出（ssh config 导出、materialize 均不受 group 影响，纯组织维度）
- 行渲染「数据→行」跨 Tab 抽象

## 安全性分析

不触碰加密与同步通道：`group` 是 per-entry 加密 JSON 内的新字段，与 tags 同级的明文语义（密文存储、零知识同步不变）；旧 client 忽略未知字段，新旧版本读写兼容。私钥遮蔽、MCP 只读等既有安全约束不变。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `ssh-assets`: Host 字段模型新增 `group`（含 CLI/TUI 编辑入口）；TUI 新增分组侧栏展示、tags 行内展示、KeyPair 引用计数与过滤、过滤/搜索匹配范围扩展

## Impact

- `internal/storage/types.go`（`HostEntry` 加字段，per-entry JSON 天然兼容）
- `cmd/ssh.go`（`host add/edit --group`）
- `internal/ssh/`（host 读写、列表排序接口）
- `internal/tui/ssh_tab.go`（三栏重构、行渲染、过滤逻辑）
- `.agents/skills/senv-cli/SKILL.md`（按 AGENTS.md 约定同步新 flag 文档）
- `CONTEXT.md` 术语已沉淀（分组/标签），无需再改
