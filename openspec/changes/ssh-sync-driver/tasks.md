## 1. 准备
- [ ] 1.1 把涉及面里角色为必须的仓（`.`）切到任务分支 `ssh-sync`（无则 `git switch -c`，有则 `git switch`；工作树 dirty 时按 Driver 协议处理）

## 2. 实施
- [x] 2.1 完成子 change `ssh-sync-channel`：apply 至全部 checkbox 勾选且 `openspec validate --strict --type change ssh-sync-channel` 通过
- [x] 2.2 完成子 change `ssh-sync-export-warning`：apply 至全部 checkbox 勾选且 `openspec validate --strict --type change ssh-sync-export-warning` 通过

## 3. 收尾
- [x] 3.1 全仓回归与静态检查（`make check`），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [ ] 3.3 提交交付仓改动
- [ ] 3.4 归档子 change `ssh-sync-channel` 与 `ssh-sync-export-warning`
