# Tasks

## 1. 准备

- [x] 1.1 把涉及面里角色为必须的仓（`.`）切到任务分支 `provider-per-shape-urls`（无则 `git switch -c`，有则 `git switch`；工作树 dirty 时按 Driver 协议处理）。验证：`git branch --show-current` 为 `provider-per-shape-urls`

## 2. 实施

- [x] 2.1 完成子 change `provider-per-shape-urls-core`：apply 至全部 checkbox 勾选且 `openspec validate --strict --type change provider-per-shape-urls-core` 通过
- [x] 2.2 完成子 change `provider-per-shape-urls-surfaces`：apply 至全部 checkbox 勾选且 `openspec validate --strict --type change provider-per-shape-urls-surfaces` 通过

## 3. 收尾

- [x] 3.1 全仓回归与静态检查（`go test ./...`、`golangci-lint run --new-from-rev=origin/main`），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准
- [x] 3.3 提交交付仓改动
- [x] 3.4 归档子 change `provider-per-shape-urls-core`、`provider-per-shape-urls-surfaces`
