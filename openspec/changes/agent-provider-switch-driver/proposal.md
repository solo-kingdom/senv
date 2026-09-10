## Why

保存多个 LLM provider，支持为多个 coding agent 切换对应 provider（含 api url 与模型）；支持从 models.dev 拉取 provider 和 model，添加时自动加载最新模型，也支持自定义模型。

## What Changes

- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals

- 不改变 senv 既有同步后端（git/server provider）的语义与实现
- 不做 LLM 请求代理/转发，senv 只负责 provider 档案管理与切换写入
- 首期不做环境变量注入式切换（改写 agent 配置文件为准）
- 不做多 profile 预设：每个 agent 单一当前指向
- 不做 project 级切换，首期仅 user 级
- claude-desktop 不支持；cursor 仅 best-effort
- Coding Agent 当前指向不做跨机同步（本机状态）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准

- [x] `senv ai refresh` 可从 models.dev 拉取并缓存 provider/model 目录，离线时可继续使用缓存
- [x] `senv ai provider add/list/show/remove` 可管理 LLM Provider 档案：凭据存 vault、档案只存引用；添加时自动从目录加载最新模型，支持自定义模型
- [x] `senv ai switch` 将指定 coding agent 指向 provider+模型并改写其配置文件；`senv ai status` 显示各 agent 当前指向
- [x] TUI 可浏览 provider 与各 agent 当前指向，并可执行切换
- [x] MCP 提供只读查询：provider 列表与各 agent 当前指向

## Driver 协议

- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `{task}-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/{task}-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-09 1.1：切出任务分支 `agent-provider-switch`（自 main）。工作树仅含本任务产出（grill 产出的 CONTEXT.md 修改与本 task 的 OpenSpec change 目录），直接切换。
- 2026-09-10 3.1：全仓回归 `make check`（fmt + vet + lint + go test -race ./...）全部通过；5 个子 change `openspec validate <name> --strict` 全部通过。
