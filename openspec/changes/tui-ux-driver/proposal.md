## Why
提升 TUI 的用户体验：统一支持分组、多选、搜索等交互能力，并补充其他易用性改造。

现状（代码事实，2026-09-11 盘点）：
- 8 个 Tab（Env/Text/Config/SSH/AI/MCP/History/Audit）各自手写列表渲染与按键处理；共享面仅有 `windowedPane`、styles、form、detail overlay。
- 多选只在 AI 切换向导（`flowSelected`）存在；过滤（`/`）只在 env/text/config/audit 存在且逻辑复制 4 份；分组只在 env/text/config 存在，config 是唯一的侧栏模式。
- 按键语义跨 Tab 冲突：`r`（rename/refresh/restore 四义）、`e`、`x`、`u`、`m`、`i` 各有多义；`g`/`G`、PgUp/PgDn 仅部分 Tab 支持；`esc` 语义不一。
- help 文案字符串是 de-facto keymap 真相源（`help.go` 解析 `Help()` 字符串）。

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 不改变 TUI 的功能范围（不新增业务能力，只统一与增强交互）
- 不替换 bubbletea/lipgloss 技术栈
- 不在本 driver 内决定具体拆分粒度（留给 propose）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] 7 个子 change（fixes/keymap/list/filter/multiselect/sidebar/forms）全部 apply 完成：各自 tasks.md 全勾且 `openspec validate --strict` 通过
- [x] 按键语义符合 grill.md D7 附录表：`r`=rename 唯一、refresh=`Ctrl+R`、env 组 `t`、text 导出 `x`、AI model-only `M`、确认框统一、help overlay 与实际键位一致
- [x] 多选/过滤/侧栏能力按 D2/D3/D4 范围可用：五类列表多选、`/` 过滤全 Tab、env/text 侧栏（All 伪组+计数）
- [x] 修复项落地：`S` 搜索结果窗口化、history `q` 过 dirty-quit 守卫、config All 伪组与 Config Tab 布局 spec 对齐
- [x] `.agents/skills/senv-cli/SKILL.md` TUI 键位小节与新交互同步；`make check` 通过

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `tui-ux-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/tui-ux-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
- 2026-09-11（分支 tui-ux，提交见 git log）：
  - 2.1 tui-ux-fixes：search 结果窗口化（visibleRange/listPageSize/truncateWidth，预算=终端-外框5行-overlay4行）；history 移除 `q→tea.Quit`；All 伪组/Config 双栏核验一致；SKILL.md 无涉；make check 通过
  - 2.2 tui-ux-keymap：keymap.go 注册表（KeyAction），Tab.Help()→Bindings()，help/状态栏同源渲染；refresh→Ctrl+R、history restore→R、env 组 t、text 导出 x、AI model-only M、plan 确认仅 esc/n 取消；g/G/PgUp/PgDn 全 Tab 补齐；SKILL.md 同步；make check 通过
  - 2.3 tui-ux-list：List 组件 + paneBudgets/renderSidebar；helpers.go 收敛；audit/history 迁移（history 窗口居中→跟随）；单元测试覆盖；make check 通过
  - 2.4 tui-ux-filter：Filter 状态机；env/text/config/audit 迁移；SSH/AI/MCP 接入 `/`（左栏主列表、右栏联动、跳转前清过滤）；测试覆盖；SKILL.md 同步；make check 通过
  - 2.5 tui-ux-multiselect：List 多选集（稳定标识）；五类列表 space/a；批量动词复用单项键走既有确认/计划流（config 合并计划、MCP 别名列表计划、text/ssh 目录批量导出）；空集回落；提交清空；标题计数提示；测试覆盖；SKILL.md 同步；make check 通过
  - 2.6 tui-ux-sidebar：renderSidebar 共享（config 等价迁移）；env/text All 伪组置顶+过滤感知计数+All 视图聚合（group/key 前缀）；All 上组操作护栏；新建/导入落组 realGroup；`→` 定位第一条；text 空分组显示；测试断言更新；SKILL.md 重写侧栏段；make check 通过
  - 2.7 tui-ux-forms：env 新建/config 创建迁移结构化表单（value 遮蔽、内联校验、reopen 回填）；手搓状态机整段移除；测试重写；SKILL.md 同步；make check 通过
  - 3.1 全仓 `make check`（fmt+vet+lint+test，`go test -race ./...`）全部通过；`go run . --help`、`go run . config --help`、`go run . mcp list-tools` 语法验证通过（SKILL.md 变更核验）
  - ADR：docs/adr/0001-self-built-tui-list-component.md 落盘（源自 grill ADR 候选）

