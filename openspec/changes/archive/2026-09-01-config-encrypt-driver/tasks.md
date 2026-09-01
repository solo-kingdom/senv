## 1. 准备
- [x] 1.1 把涉及面里角色为必须的仓切到任务分支 `feat/config-encrypt`

## 2. 实施
- [x] 2.1 完成子 change `config-encrypt-storage`：apply 至全部 checkbox 勾选且 validate --strict 通过
- [x] 2.2 完成子 change `config-encrypt-install`：同上（依赖 storage 的模型与展开能力）
- [x] 2.3 完成子 change `config-encrypt-uninstall`：同上（依赖 install 的 plan 框架）
- [x] 2.4 完成子 change `config-encrypt-tui`：同上（依赖 storage/install/uninstall）

## 3. 收尾
- [x] 3.1 全仓回归与静态检查（make check），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [x] 3.3 提交交付仓改动
- [x] 3.4 归档全部子 change
