## 1. 准备

- [x] 1.1 把涉及面里角色为必须的仓（.）切到任务分支：无则 `git switch -c tui-review-fixes`，已有则 `git switch`；工作树 dirty 时按 Driver 协议处理

## 2. 实施

- [x] 2.1 完成子 change `tui-review-fixes-core`：apply 至全部 checkbox 勾选且 `openspec validate --strict` 通过
- [x] 2.2 完成子 change `tui-review-fixes-hygiene`：同上

## 3. 收尾

- [x] 3.1 全仓回归与静态检查（`make check`），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [x] 3.3 提交交付仓改动
- [ ] 3.4 按 core → hygiene 顺序归档全部子 change（`openspec archive`）
