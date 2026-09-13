## 1. 模型与 CLI

- [x] 1.1 `internal/storage/types.go`：`HostEntry` 新增 `Group string`（json tag `group,omitempty`），更新字段注释；确认 `internal/storage/ssh.go` 读写无需改动（per-entry JSON 兼容）
- [x] 1.2 `internal/ssh/`：host 读写/编辑接口贯通 `Group`（add/edit 的字段映射与校验，与 tags 同等处理）
- [x] 1.3 `cmd/ssh.go`：`senv ssh host add` 与 `edit` 增加 `--group` flag；`host list` 输出展示 group（有值时）
- [x] 1.4 补 CLI 测试：`--group` 写入/编辑/空值回退（参照 `cmd/ssh_test.go` 既有用例）

## 2. TUI 三栏重构

- [x] 2.1 `internal/tui/ssh_tab.go`：改为「分组侧栏 → Host 列表 → KeyPair 列表」三栏，复用 `renderSidebar`/`SidebarRow`（参照 `env_tab.go` 侧栏接入方式）；组排序 All 置顶 → 字母序 → 「未分组」置底，「未分组」仅在有未归类 host 时出现
- [x] 2.2 侧栏接入 Tab 状态：选中组决定中间栏 host 集合，切组重置中间栏光标；`SetSize` 三栏宽度分配（KeyPair 栏最小宽度下限）
- [x] 2.3 Host 表单加 `Group` 自由文本字段（`internal/tui/form.go` 结构体表单，失焦不校验，与 alias 同等）

## 3. 行渲染与过滤

- [x] 3.1 `hostListLabel`：行尾追加 tags（`#tag` 前缀、最多 2 个 + `+n`），用 `truncateWidth` 防折行；行内不渲染 group
- [x] 3.2 `keyPairListLines`：行内引用计数 `被 N 个 Host 引用`（复用 `hostRefs()`），零引用灰显「未被引用」，排序保持名字序
- [x] 3.3 Host 栏 `/` 过滤匹配 alias + hostname + tags + group；KeyPair 栏新增 `/` 过滤（按名称，含焦点切换逻辑）
- [x] 3.4 `internal/tui/search.go`：SSH 类目全局搜索纳入 group + tags 匹配

## 4. 测试与文档

- [x] 4.1 `internal/tui/ssh_tab` 测试：侧栏组切换/未分组兜底/All 伪组、tags 行内渲染（含 +n 截断）、引用计数与零引用、双侧栏过滤、四维匹配
- [x] 4.2 `go run . --help`、`go run . ssh host add --help` 验证 flag 注册；`make check` 全绿
- [x] 4.3 更新 `.agents/skills/senv-cli/SKILL.md`：`--group` flag 与 list 输出变化（AGENTS.md 约定）
