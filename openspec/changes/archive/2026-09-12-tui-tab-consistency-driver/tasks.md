## 1. 准备

- [x] 1.1 把涉及面里角色为必须的仓（.）切到任务分支：无则 `git switch -c tui-tab-consistency`，已有则 `git switch`；工作树 dirty 时按 Driver 协议处理（keymap `Group` 重构落地前不得开切）
  - 2026-09-12：keymap 重构与 openspec 规划分两笔提交（24a7729、888b733）后自 tui-ux 开切 tui-tab-consistency

## 2. 实施

- [x] 2.1 完成子 change `tui-tab-consistency-render`：apply 至全部 checkbox 勾选且 `openspec validate --strict` 通过

## 3. 收尾

- [x] 3.1 全仓回归与静态检查（`make check`），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [x] 3.3 提交交付仓改动
- [x] 3.4 归档子 change `tui-tab-consistency-render`（`openspec archive`）
