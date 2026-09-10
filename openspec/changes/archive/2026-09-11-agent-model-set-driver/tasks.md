## 1. 准备

- [x] 1.1 把涉及面里角色为必须的仓切到任务分支（工作树存在无关未提交改动时按 Driver 协议列出路径并确认；用户决定留在 main，不切分支）

## 2. 实施

- [x] 2.1 完成子 change `agent-model-set-store`：apply 至全部 checkbox 勾选且 `validate --strict` 通过
- [x] 2.2 完成子 change `agent-model-set-agents`：apply 至全部 checkbox 勾选且 `validate --strict` 通过
- [x] 2.3 完成子 change `agent-model-set-cli`：apply 至全部 checkbox 勾选且 `validate --strict` 通过
- [x] 2.4 完成子 change `agent-model-set-tui`：apply 至全部 checkbox 勾选且 `validate --strict` 通过

## 3. 收尾

- [x] 3.1 全仓回归与静态检查（`make check`），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [x] 3.3 提交交付仓改动（用户确认「一块提交」：与本工作树内另一任务的 session 改动合并为一次提交）
- [x] 3.4 归档全部子 change（先子后 driver）
