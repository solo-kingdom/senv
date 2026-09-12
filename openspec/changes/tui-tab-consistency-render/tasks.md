## 1. AI/MCP/SSH 加载态

- [ ] 1.1 `ai_tab.go`：`viewBaseAt` 增加 `!t.loaded` 守卫，两栏常驻几何内嵌「加载 provider 中…」（emptyStateStyle），空态分支仅在装载后触达；更新 `ai_tab_test.go` 受影响断言并补装载期渲染用例
- [ ] 1.2 `mcp_tab.go`：同构守卫「加载 MCP 档案中…」，plan/changed-confirm 模式不受影响；更新 `mcp_tab_test.go` 并补用例
- [ ] 1.3 `ssh_tab.go`：同构守卫「加载 SSH 资产中…」（双栏）；更新 `ssh_tab_test.go` 并补用例

## 2. History/Audit 面板几何

- [ ] 2.1 `history_tab.go`：View 改为 `windowedPane` + 底部附加行（detail/confirm/flash）+ `clipLines` 收口 + `paneStyle.Width/Height`；`SetSize` chrome 预算重算；更新 `history_tab_test.go`/`history_tab_lazy_test.go` 并补几何与装载期用例
- [ ] 2.2 `audit_tab.go`：同构改造（底部附加行：skipped/filter 提示；`loadErr` 分支内嵌补几何）；更新 `audit_tab_test.go` 并补用例
- [ ] 2.3 resize 跟随回归：History/Audit 在尺寸变化后面板宽度等于新内容区宽（小终端含 30×7 下限冒烟）

## 3. 收尾

- [ ] 3.1 `make check` 全绿；核对 `.agents/skills/senv-cli/SKILL.md` 无涉及加载态/几何的描述需同步（无则记录免改）
- [ ] 3.2 `openspec validate --strict --type change tui-tab-consistency-render` 通过
