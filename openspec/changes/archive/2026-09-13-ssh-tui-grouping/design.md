## Context

SSH Tab（`internal/tui/ssh_tab.go`）目前是双栏：左栏 Host 按别名字典序平铺（`hostListLabel`），右栏 KeyPair 按名平铺（`keyPairListLines`），无任何分组。`HostEntry`（`internal/storage/types.go:101`）已有 `Tags []string` 但没有 group 字段。Env/Text/Config 三个 Tab 已有「分组侧栏 + 条目」三栏范式，共享组件在 `internal/tui/list.go`（`renderSidebar`/`SidebarRow` 约 322-343 行、`windowedPane` 136 行），ADR-0001 规定列表组件自研并已逐步下沉共享。Host↔KeyPair 引用关系已有现成计算（`hostRefs()`，`internal/tui/ssh_tab.go:671`）。存储为 per-entry 加密 JSON（`internal/storage/ssh.go`），新增字段天然前向兼容。

## Goals / Non-Goals

**Goals:**
- SSH Tab 交互与 Env/Text/Config 分组范式完全一致，复用 `renderSidebar`，零新交互范式
- `group` 字段贯通存储 → CLI（`senv ssh host add/edit --group`）→ TUI 表单 → 展示/过滤
- Tags 从「死字段」变成可见、可过滤的一等标注

**Non-Goals:**
- 不改 AI/MCP Tab（后续 change 复用范式）
- 不做 KeyPair 分组、不做行渲染跨 Tab 抽象
- 不触碰加密/同步/syncschema（`group` 在密文 JSON 内部，协议不变）

## Decisions

### D1: 新增单值 `Group` 字段，而非拿 Tags 当分组

**选择**：`HostEntry` 增加 `Group string`（json tag `group,omitempty`），空 = 未分组。

**理由**：Tags 是多值标注，分组侧栏是单值归属语义（Env/Text/Config 的 group 均如此）；一个 host 在多个组下重复出现会破坏侧栏交互一致性。`group` 与 env/text/config 的 group 同名同语义，全局搜索、导出等既有 group 概念的调用方零认知成本。

**备选**：Tags 直接当分组（零模型改动）——多值与单值语义冲突，弃。

### D2: 三栏重构复用既有共享组件，不自研折叠组

**选择**：SSH Tab 从双栏改为「分组侧栏 → Host 列表 → KeyPair 列表」，侧栏直接用 `renderSidebar`/`SidebarRow`；组排序：All 置顶 → 组名字母序（带条目数）→ 「未分组」置底；组内 Host 别名字典序。

**理由**：ADR-0001 的既定方向就是「分组/多选/过滤三类交互一致」；复用侧栏组件用户零学习成本。未分组置底是对 Env 范式的唯一偏离——空 group 是「还没归类」而非「默认组」，语义不同。

**备选**：列表内折叠组标题（省横向空间）——新交互范式需自研，弃。

### D3: KeyPair 不加分组，用引用计数补关联可见性

**选择**：KeyPair 栏保持名字序平铺，行内加 `被 N 个 Host 引用`（复用 `hostRefs()`），零引用灰显「未被引用」；补 `/` 过滤（按名称）。

**理由**：KeyPair 数量通常远小于 Host，分组收益低；「谁在用这把 key」才是高频问题，引用关系数据已有。

**备选**：KeyPair 也加 `Group` 字段（与 Host 对称）——对实际问题无帮助，弃。

### D4: Tags 行内渲染上限 2 个 + `+n`，窄屏截断

**选择**：Host 行尾追加 `#tag1 #tag2 +n`，用既有 `truncateWidth` 防折行。

**理由**：行内空间已紧（`alias → user@host:port key:name(fp)`），无限展示会挤掉关键信息；2 个 + 截断是可预期的稳定布局。

### D5: 过滤/搜索四维匹配

**选择**：`/` 匹配 `alias + hostname + tags + group`；`S` 全局搜索的 SSH 类目同样纳入 group/tags。

**理由**：Q1 轮用户明确圈了「tags 不参与过滤」为要修的问题；group 进了模型就应与 tags 同等待遇。匹配实现与现有 `alias + " " + hostname` 拼接同一模式，扩展成本一行。

## Risks / Trade-offs

- [旧 client 读写含 `group` 的数据] → per-entry JSON 编码，`group,omitempty` 缺省即旧形态；旧 client 忽略未知字段，已验证的兼容模式（tags 字段当年同样路径引入）。
- [用户手输 `prod`/`Prod` 造出近重复组] → 接受。与 alias 等其他字段同等不做强校验；侧栏字母序下近重复组相邻可见，属于用户自治范围。
- [三栏横向更挤] → KeyPair 栏设最小宽度下限，低于时 tags 片段优先被截断（D4 的 `truncateWidth` 已覆盖）。
- [TUI 测试量] → `ssh_tab` 已有测试基线，三栏改动需同步补侧栏选择/过滤/引用计数的用例；tasks 里专列。

## Migration Plan

无需数据迁移：`group` 缺省 = 未分组，既有 host 全部落入「未分组」兜底组（仅在有未归类 host 时显示）。CLI 与 TUI 随版本发布自然生效。回滚 =  revert 本 change，旧版本读写均不受含 `group` 的数据影响。
