## 1. 计划与执行
- [x] 1.1 UninstallPlan/PlanUninstall：noop / delete / changed / error 动作判定（复用解密与展开）
- [x] 1.2 ExecuteUninstall：执行前重新比较，仅删除文件不删目录；单测覆盖四种动作

## 2. CLI
- [x] 2.1 `config uninstall [name] [--group g|--all]`，plan → 确认 → 执行；`--dry-run`、`--yes`
- [x] 2.2 changed 条目逐条二次确认交互；plan 渲染中 changed 醒目标注

## 3. 收尾
- [x] 3.1 端到端测试：install → uninstall（未改动直删、改动需确认）→ 再 install 恢复
- [x] 3.2 `make check` 通过
