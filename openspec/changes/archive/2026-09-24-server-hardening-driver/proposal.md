## Why
senv-server 公网加固（探索任务 server-hardening 的已决方案）：审计日志与业务数据同库同角色可被抹除、admin CLI 不留痕、无异常告警、token 哈希无 pepper、部署加固无文档。四项最小高收益组合已冻结，约束：单人/小团队自用威胁模型、单实例部署、接受双 DSN + cron 运维 `logs-prune`。

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 不改零知识架构本身（密文托管模型不动）
- 缓议不做：per-token 限速、存储配额、token TTL/轮换、hash chain、mTLS

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | senv 仓：server 代码、迁移、admin CLI、docs/senv-server.md |

## 验收标准
- [x] DB 角色隔离 + admin 操作入审计：serve 运行时角色对 access_log 仅 INSERT+SELECT，logs-prune 用单独 admin 角色（cron 示例），admin 子命令（create-user/revoke-token/create-registration/block/unblock）写审计记录
- [x] 通用 webhook 告警：server 支持可配置 webhook URL，异常事件 POST JSON（连续 AUTH-FAILED 超阈值、BLOCKED、新 client 注册成功、client 换 IP 首次访问）；不内置第三方 provider
- [x] token HMAC pepper：SHA-256 改 HMAC，pepper 来自环境变量/文件，不进 DB；旧 token 失效有重新签发流程
- [x] 部署加固文档写入 docs/senv-server.md：systemd 加固、反代推荐配置（HSTS/limit_req/TLS 1.2+）、双 DSN cron 示例、单实例边界、元数据泄露边界（vault 名/同步频率可见，落点小节实现期定）
- [x] 新增或变更 cmd/ 命令、flag、交互或安全行为时同步更新 .agents/skills/senv-cli/SKILL.md，并验证 go run . --help、go run . <command> --help、go run . mcp list-tools

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `server-hardening-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 切任务分支只针对涉及面里角色为 `必须` 的修改仓，且本身可选；task 所在仓不是修改仓时不切。修改仓干净：先 fetch 默认分支，再从其最新提交 `git switch -c`（分支已存在则 `git switch`）。修改仓 dirty：列出未提交路径，由用户三选一——不切直接在当前分支修改 / 携带改动 `git switch` / `git worktree add` 从默认分支最新提交建独立工作树。不得 stash / reset / 强制切换。用户未选择、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
- 2026-09-24：四个子 change 全勾且 `openspec validate --strict` 通过；`go test ./internal/server/...` 全绿；`go run . --help` / `go run . mcp list-tools` 验证通过；flag/env 名已按实现核对（docs/senv-server.md「公网加固」节交叉引用一致）
