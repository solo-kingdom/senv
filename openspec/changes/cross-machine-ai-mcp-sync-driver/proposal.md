## Why

`senv` 的同步通道（`internal/syncschema`）目前覆盖 `env` / `env_meta` / `text` / `config` / `config_index` 五类，但 LLM Provider 档案（`dataPath/llm_providers/<alias>.enc`）与 MCP Server 档案（`dataPath/mcp_servers/<alias>.enc`）不在内。它们是人工添加进 vault 的"配置源"，与 env/text 同形态——但目前每台机器必须各自重新 `senv ai provider add` / `senv mcp add` 才能用，结果是同一份配置在多台机器之间人为分裂。同时，agent 切换指针与 MCP 导出 ledger 是 per-machine state（CONTEXT.md 与 ADR-0003 / 0007 已明示），仍保持本地、不进 vault。本 driver 把"配置源同步、本机状态不同步"这条边界钉死。

## What Changes

- 本 change 是 taskflow driver，不直接改代码，只编排子 change
- `internal/syncschema/schema.go` 增补两个 kind：`llm_provider`、`mcp_server`
- `internal/provider/server_state.go::entryLocation` 与 `collectEntriesDiff` 增列 `LLMProviderDirName` / `MCPServerDirName`，复用现有 SSH-style 加密 blob 路径
- 冲突策略沿用现有 LWW + revision；`llm_provider` / `mcp_server` 冲突时多打一条 warning 到 stderr / TUI audit
- 首次 bootstrap：直接随 sync 拉到本机，不加 opt-in；保留 `--accept-remote` 重建
- 凭据引用跨机解析失败：`senv ai switch` 与 `senv mcp export` 都 fail-closed + 给诊断；MCP export 时把缺失的 env/text 名列入 warning 但仍写入字面量（与 ADR-0008 的明文落盘事实并存）
- TUI audit 面板增"since last pull"子视图，复用现有面板与 sync state 元数据，不开新面板
- `CONTEXT.md` 在 driver apply 阶段同步更新：明示"人工添加的配置源"为可同步、"本机状态"为不同步；不触动既有 ADR-0003 / 0007 / 0012 的边界
- ADR 候选 `sync-ai-mcp-source-of-truth` 转正为 `docs/adr/0018-sync-ai-mcp-source-of-truth.md`

## Non-goals

- 不动 agent 切换指针（`agent-pointers.json`）与 MCP 导出 ledger（`mcp-exports.json`）的存储位置与不同步状态——ADR-0003 / 0007 不变
- 不动 LLM Provider / MCP Server 档案的 schema、凭据引用语义、{{env:...}} / {{text:...}} 解析规则（ADR-0002 / 0006 / 0008 不变）
- 不动 `senv-server` / git remote provider 的同步主流程；本 change 走现有 5 kind 的同一通道
- 不在本 change 范围内把 `ssh_host` / `ssh_keypair` 也接入同步通道——同形态但暂未点名，留给后续 change（已识别但延后）
- 不加 `senv ai pull` / `senv mcp pull` 等显式拉取命令——首次 bootstrap 直接随 sync 拉（决策 D6）
- 不引入凭据代理/转发（MCP secret 仍按 ADR-0008 在 export 时明文落盘）
- 不做 reconcile 流程自动重建 MCP export ledger——ADR-0007 的"漂移"语义保留
- 不改 auto-sync / AutoPull / AutoPush 触发策略；新 kind 走现有触发面

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准

- [ ] `internal/syncschema` 增补 `KindLLMProvider` / `KindMCPServer` 常量与 `ValidateIdentity` 分支；既有 5 kind 行为不变
- [ ] `internal/provider/server_state.go` 的 `entryLocation` 与 `collectEntriesDiff` 增列 `LLMProviderDirName` / `MCPServerDirName`；既有 env/text/config/config_index 扫描行为不变
- [ ] `llm_provider` / `mcp_server` 冲突时 stderr / TUI 输出诊断，含"本地 vs 远端"的 alias / revision 对照；LWW 主体行为与 env/text 一致
- [ ] 新机器首次 sync 后本地出现来自远端的所有 `llm_providers/<alias>.enc` 与 `mcp_servers/<alias>.enc`；`senv ai provider list` / `senv mcp list` 可见
- [ ] `senv ai switch` 在本机缺 `credential_ref` 指向的 text 时 fail-closed，错误诊断指明缺失的 `text:llm-keys/<alias>`
- [ ] `senv mcp export` 在本机缺模板引用时仍写入（保持向后兼容），但把缺失 env/text 名列入 warning
- [ ] TUI audit 面板新增"since last pull"子视图，能列出上一轮 sync 引入/覆盖的 ai_provider / mcp_server 清单
- [ ] `CONTEXT.md` 在同步通道边界处显式声明"人工添加的配置源同步、本机状态不同步"；既有"当前指向" / "导出状态"等本机状态条目不动
- [ ] `docs/adr/0018-sync-ai-mcp-source-of-truth.md`（proposed）落地；其 Considered Options / Consequences 段覆盖 ssh 延后项
- [ ] `.agents/skills/senv-cli/SKILL.md` 同步更新；`make check` 通过

## Driver 协议

- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `{task}-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/{task}-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-12 grill：10 项决策 settled（见 `grill.md`），1 项 ADR 候选 `sync-ai-mcp-source-of-truth` 待 propose 阶段晋升为 `docs/adr/0018`。