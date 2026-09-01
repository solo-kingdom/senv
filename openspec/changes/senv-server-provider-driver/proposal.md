## Why
探索新增 senv-server 的可能性：现在是基于 git + 本地仓库的模式，新增 senv server provider，数据保存在数据库内

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 本阶段只做探索与方案收敛，不落地实现代码

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [ ] 明确 senv server provider 的目标形态（与现有 git + 本地仓库模式的关系：替代 / 并存）
- [ ] 明确数据模型与数据库选型（schema、迁移策略、嵌入库 vs 外部服务）
- [ ] 明确 server 的接口形态（HTTP API / gRPC / CLI 对接方式）与鉴权方案
- [ ] 明确与现有 provider 抽象的对接点（代码改动面清单）
- [ ] 探索结论与决策写入 driver proposal 验证记录，作为后续 propose 阶段的输入

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `senv-server-provider-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/senv-server-provider-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
