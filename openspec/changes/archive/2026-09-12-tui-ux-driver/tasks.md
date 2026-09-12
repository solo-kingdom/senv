## 1. 准备

- [x] 1.1 把涉及面里角色为必须的仓（.）切到任务分支：无则 `git switch -c tui-ux`，已有则 `git switch`；工作树 dirty 时按 Driver 协议处理

## 2. 实施

- [x] 2.1 完成子 change `tui-ux-fixes`：apply 至全部 checkbox 勾选且 `openspec validate --strict` 通过
- [x] 2.2 完成子 change `tui-ux-keymap`：同上
- [x] 2.3 完成子 change `tui-ux-list`：同上
- [x] 2.4 完成子 change `tui-ux-filter`：同上
- [x] 2.5 完成子 change `tui-ux-multiselect`：同上
- [x] 2.6 完成子 change `tui-ux-sidebar`：同上
- [x] 2.7 完成子 change `tui-ux-forms`：同上

## 3. 收尾

- [x] 3.1 全仓回归与静态检查（`make check`），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [x] 3.3 提交交付仓改动
- [x] 3.4 按 ①→⑦ 顺序归档全部子 change（`openspec archive`，顺序即 spec delta 基线，不得乱序）
