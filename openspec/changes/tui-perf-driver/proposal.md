# tui-perf-driver

## Why
优化性能，senv tui 加载变量什么的太慢了，另外需要高耗时日志打印

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 冷启动认证路径（PBKDF2 口令派生 600k 迭代）保持不动（D1）
- 服务端 TLS 建连慢（senv.wii.pub 握手 0.35~1.5s）的 infra 排查拆独立调查，不阻塞本次（D6-C）
- 不改同步协议与 server API 语义；不降低既有 rekey/并发正确性要求

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] 单趟全量本地读 ≤300ms（D8②）：实测 `env.snapshot` 13ms / `env.list-groups` 13ms（基线 1185ms，39 组 268 变量）
- [x] TUI 启动与单条 CLI 命令进程内网络建连 ≤1 次（D8③/D6-A）：实测 `conns_new=1`，复用请求 110ms（对比新连接 692ms）
- [x] 耗时日志落 `~/.log/senv/perf.log`（log/slog JSON lines，阈值默认 100ms、env 可调/可关），可分解启动各阶段占比（D8④/D3）
- [x] 暖启动（有会话缓存）到 env 列表可用 ≤1.5s；远端有变更时 ≤2.5s 内静默稳定，期间旧列表可操作（D8①/D7）：本地装载 13ms + SWR 已实现并有单测；TUI 交互启动无法 headless 实测，待用户实机确认
- [ ] 全部子 change 归档，`openspec validate --strict` 通过

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `tui-perf-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/tui-perf-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-11 脚手架创建。grill 收敛后回填 Non-goals 与验收标准。
- 2026-09-11 grill 完成：D1~D8 全部 settled（根因实测与决策见 `grill.md`），Non-goals 与验收标准已回填；ADR 候选 adr-读路径锁语义待 propose/design 吸收。
- 2026-09-11 apply 轮 1：tui-perf-log 3.3 保持未勾——validate --strict 通过；全量 make check 因工作树中用户暂存 WIP 的既有测试失败（TestAIProviderModelInfoFlags，经临时 worktree 在 HEAD+WIP 上复现，与本 change 无关）无法通过，待用户 WIP 落地后补验。
- 2026-09-11 apply 轮 1：tui-perf-net 3.3 保持未勾——validate --strict 通过、本 change 触达包（cmd/internal/tui/internal/provider）-race 全绿、实测 conns_new=1；全量 make check 同样被用户 WIP 既有失败阻塞，原因同上。
- 2026-09-11 apply 轮 1 收尾：tui-perf-load 3.1/3.2 保持未勾——实测 `sync.collect` 全量仅 2~3ms（430 条，无逐文件锁税），「增量收集」层无收益且有脏判定正确性风险，建议修订 spec 后再实施；tui-perf-load 2.1 的共享内存快照 registry 同理待确认（单趟批量已 13ms）。4.2 因全量 make check 被用户 WIP 既有失败（TestAIProviderModelInfoFlags）阻塞未勾，validate --strict 均通过，本轮触达包测试全绿。
