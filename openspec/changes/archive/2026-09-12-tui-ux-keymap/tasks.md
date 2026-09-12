## 1. 注册表基建

- [x] 1.1 新建 `internal/tui/keymap.go`：`Binding{Keys, Action, Desc}` 与 Tab 键位声明接口，`help.go` 改由注册表渲染、删除 `parseHelp` 字符串解析；验证：`go build` 通过，`?` overlay 内容与迁移前等价（除本 change 键位裁决外）
- [x] 1.2 `model.go` 全局键（`1-9`/`tab`/`q`/`S`/`?`）迁入注册表，新增全局 `Ctrl+R` 刷新动作；验证：数字直达、`Ctrl+R` 触发当前 Tab Reload

## 2. 键位裁决落地（grill D7 附录）

- [x] 2.1 refresh 让出 `r`：ssh/ai/audit 的列表刷新键改 `Ctrl+R`；history restore 改 `R`；验证：各 Tab 按 `r` 走 rename（无 rename 语义的 Tab 按注册表声明忽略），`Ctrl+R` 刷新，history `R` 弹恢复确认
- [x] 2.2 env 组激活/停用统一 `t`（toggle，default 分组不可停用）；验证：未激活组 `t` 出现 `●`，已激活非默认组 `t` 移除，default 组 `t` 提示不可停用
- [x] 2.3 text 导出键 `o` 改 `x`；验证：`x` 弹导出路径表单并写文件
- [x] 2.4 AI model-only 键 `m` 改 `M`；验证：`M` 进默认模型选择（候选限已写入集合），未指向时提示先按 `s`；`m` 不再触发该流程
- [x] 2.5 确认框统一 `y`/`enter` 确认、`esc`/`n` 取消；config/MCP plan 页收紧为仅 `esc`/`n` 取消、其余键忽略（`y`/`enter` 确认、`F` 覆盖漂移不变）；验证：plan 页按任意字母键无效果，`esc`/`n` 取消不写盘
- [x] 2.6 `esc`=回上一层规则写入 help overlay 全局段；`g`/`G` 跳顶/底补齐 SSH/AI/MCP（并按 D7 全局动词表补齐 audit/history）；验证：三 Tab `g`/`G` 生效，help 中可见 esc 规则
- [x] 2.7 （propose 漏分配，apply 时补录）PgUp/PgDn 翻页补齐 env/text/config/ssh/ai/mcp/history（audit 已有）；验证：各 Tab PgUp/PgDn 游标整页移动且窗口跟随

## 3. 文档与回归

- [x] 3.1 同步 `.agents/skills/senv-cli/SKILL.md` TUI 键位小节（`r`/`Ctrl+R`/`t`/`x`/`M`/确认框/esc 规则）；验证：文档与 `?` overlay 一致
- [x] 3.2 `make check` + 全 Tab 高频动作冒烟（n/e/r/d/enter/esc/t/x/M/Ctrl+R/g/G），结果写入 proposal 验证记录；验证：退出码 0 且无静默失效按键
