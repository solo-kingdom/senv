## 1. 实现修复

- [x] 1.1 `searchTab` 结果列表接入 `windowedPane` 窗口化：结果转为渲染行、跟随 `listPageSize` 窗口、超宽行按显示宽截断；验证：构造 >100 条跨类型匹配结果，`S` 搜索浏览全部结果无溢出外框、无折行
- [x] 1.2 移除 `history_tab.go` 内的 `q → tea.Quit` 分支，退出统一由 `model.go` 顶层 dirty-quit 守卫处理；验证：server 模式有待推送条目时在 History Tab 按 `q`，先出现待推送提示再退出
- [x] 1.3 核验 All 伪组整组 install/uninstall 与 Config Tab 双栏实现和新 spec 一致，发现偏差修代码；验证：焦点在侧栏 All 上触发 install 弹出全量条目计划，确认后执行

## 2. 文档与回归

- [x] 2.1 检查并同步 `.agents/skills/senv-cli/SKILL.md`（All 伪组行为描述如涉及）；验证：`go run . --help` 与文档无冲突
- [x] 2.2 `make check` 通过，结果写入 proposal 验证记录；验证：退出码 0
