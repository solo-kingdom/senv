## 1. 模型与 CLI

- [x] 1.1 `internal/storage/types.go`：`KeyPairEntry` 新增 `Group string`（json tag `group,omitempty`），注释语义与 `HostEntry.Group` 对齐
- [x] 1.2 `internal/ssh/`：keypair 导入/更新接口贯通 `Group`；确认 storage 层零改动
- [x] 1.3 `cmd/ssh.go`：`senv ssh keypair import` 增加 `--group`；`keypair list` 输出有值时展示 group
- [x] 1.4 补测试：`--group` 写入、旧数据（无 group 字段 JSON）读写兼容；参照 change ssh-tui-grouping 的同类测试

## 2. KeyPair Tab 与 SSH Tab 瘦身

- [x] 2.1 新建 `internal/tui/keypair_tab.go`：分组侧栏（复用 renderSidebar；All 置顶 → 字母序 (n) → 未分组置底，无未归类不显示）→ KeyPair 列表（名称/指纹/引用计数 `被 N 个 Host 引用`、零引用灰显「未被引用」、名字序不受计数影响）→ `/` 名称过滤；`Title()` 返回 "KeyPair"；实现 `Tab` 接口全部方法
- [x] 2.2 迁移 keypair 动作到 keypairTab：导入（表单含 group 字段）、重命名、删除（含被引用拒绝/强制清引用语义）、materialize（含确认语义）、详情浮层；`hostRefs()` 随迁
- [x] 2.3 `keypairTab` 提供 group 编辑入口（列表上按 `e` 打开仅含 group 的表单）
- [x] 2.4 `internal/tui/model.go`：`mgr.SSH != nil` 时 `newSSHTab` 之后注册 `newKeyPairTab`
- [x] 2.5 `internal/tui/ssh_tab.go` 瘦身：删除 KeyPair 栏、keypair 动作、keypair 过滤与 focus 三态（降为侧栏/Host 两态）、keypair 详情；Host 行保留 `key:name(fp)` 内联引用；`/` 四维匹配不变

## 3. 测试与文档

- [x] 3.1 新增 `internal/tui/keypair_tab_test.go`：侧栏组浏览/All/未分组兜底、引用计数与零引用灰显、名称过滤、group 编辑；对齐 spec 场景
- [x] 3.2 适配既有测试：tab 数量/注册顺序断言（数字键场景）、ssh_tab 三栏相关断言、review_fixes/filter 测试中 keypair 栏焦点引用
- [x] 3.3 `go run . --help`、`go run . ssh keypair import --help` 验证；`make check` 全绿
- [x] 3.4 更新 `README.md`（TUI Tab 描述）与 `.agents/skills/senv-cli/SKILL.md`（KeyPair Tab 键位、`keypair import --group`、list 输出）
