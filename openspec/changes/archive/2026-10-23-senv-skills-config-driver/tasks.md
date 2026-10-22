# Tasks

## 1. 准备

- [x] 1.1 给涉及面里角色为必须的修改仓（`.`）建 worktree `worktrees/senv-skills-config`（基线与命令见 Driver 协议），之后改代码、测试、提交都在 worktree 里做。验证：`git -C worktrees/senv-skills-config branch --show-current` 为 `senv-skills-config`

## 2. 实施（一片一条，依赖写在括号里）

- [x] 2.1 完成子 change `senv-skills-config-core`：skill/skill_index 存储与 sync 身份、server skill 密文上限、rekey/doctor/orphan、CLI add/list/show/remove；apply 至全部 checkbox 勾选、`validate --strict` 通过、提交到任务分支
- [x] 2.2 完成子 change `senv-skills-config-apply-e2e`（依赖 2.1）：`skill apply` 双根落盘与冲突策略 + 判据 Go 测试（HOME_A→sync→移走 A→HOME_B apply）；同 2.1 验收节奏
- [x] 2.3 完成子 change `senv-skills-config-docs`（依赖 2.2）：仓内 senv-cli + README，整目录镜像 `~/code/.agents/skills/senv-cli`；含 server 升级顺序与体积上限说明；同 2.1 验收节奏

## 3. 收尾

- [x] 3.1 全仓回归与静态检查，命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [x] 3.3 在 worktree 里把收尾改动提交到任务分支 `senv-skills-config`（不 push）
- [x] 3.4 归档前置校核：子 change 全勾且 validate 通过、findings 全有处置
- [x] 3.5 归档全部子 change：逐个 `openspec archive senv-skills-config-<slice> --yes`（3.4 过了才跑）
