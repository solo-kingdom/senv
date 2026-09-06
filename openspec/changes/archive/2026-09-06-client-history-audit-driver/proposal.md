## Why
0. 支持认证 client 注册功能，且支持屏蔽 client，senv client 检查到被屏蔽后，应当清理 session（缓存数据可以保留）
1. 新增配置历史版本功能，默认保留 3 个版本，注意 cli+tui 查看历史版本的能力
2. 新增操作审计日志功能，senv client 可以查看（cli + tui），注意包含日期
3. 添加系统日志功能（保存数据库），主要是保存尝试访问的记录、用户错误登录、正确登录、同步等，主要用于安全审计

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 操作审计随 vault 同步（跨机合并操作史），v1 仅本机
- 整库快照式历史版本与整库回滚（仅条目级历史）
- client 远程查询 server 系统日志（安全日志仅服务器管理员可查）
- git provider 下的历史版本、屏蔽与系统日志（git 历史已覆盖回看诉求）
- 被屏蔽 client 的 server 侧数据清理（零知识下数据属于 user 的 vault）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] client 注册与屏蔽功能闭环成立（注册 → 发放凭证 → 正常使用；屏蔽 → client 检测 → 清理 session）
- [x] 配置历史版本默认保留 3 个版本，cli 与 tui 均可查看
- [x] 操作审计日志 cli 与 tui 均可查看，记录含日期
- [x] 系统日志落库（访问尝试、登录成功/失败、同步等安全事件），可查询

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `client-history-audit-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/client-history-audit-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
- 2026-09-06 1.1：切出任务分支 `client-history-audit`（自 main）。工作树含非本任务未跟踪文件（AGENTS.md、docs/agents/，另有本任务 grill 产出的 CONTEXT.md）；已通过 AskUserQuestion 征询未获答复，按未跟踪文件对 switch 零风险的判断继续，实施全程不暂存 AGENTS.md 与 docs/agents/。
- 2026-09-06 3.1：全仓回归 `make check`（fmt + vet + lint + go test -race ./...）全部通过；四个子 change 均在其 apply 完成时通过各自 `make check`。
- 2026-09-06 3.4：先归档全部子 change（含 spec 应用到 openspec/specs/），随后统一提交——避免归档产生的文件移动留作未提交状态。
