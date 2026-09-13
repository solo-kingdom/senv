## Why
hosts、keypairs 也进行数据同步：按已定稿的 ADR-0020 把 ssh_host/ssh_keypair 接入同步通道（白名单扩到九 kind、身份=别名、档案 blob 含私钥、host export 缺失引用逐条 warning、冲突报告仅元数据渲染）。

设计已定稿，无需再澄清：`docs/adr/0020-ssh-assets-in-sync-channel.md`（决策与 rejected 备选）、`docs/adr/0019-sync-ai-mcp-source-of-truth.md` 尾部修正注记（「档案 blob 不内嵌凭据」口径）、`CONTEXT.md`「配置源」扩展与「落盘（Materialize）」术语。实现要点：

- `internal/syncschema/schema.go`：加 `KindSSHHost` / `KindSSHKeypair`，`ValidateIdentity` 对应分支（grp 空、key=别名，同 `llm_provider`/`mcp_server` 的 alias 模式）
- `internal/provider/server_state.go`：`collectEntriesDiff` 增加对 `storage.HostDirName`（hosts/）、`storage.KeypairDirName`（keypairs/）两个目录的收集；`entryLocation` 增加两个落地 case（`hosts/<alias>.enc`、`keypairs/<name>.enc`）
- `internal/ssh/host.go`（export）：`IdentityKey` 引用的 keypair 不在本机 vault 时逐条 warning、照常写出（`mcp export` 宽松写入同模式）
- `internal/conflict/render.go`：`ssh_host`/`ssh_keypair` 仅元数据渲染（别名/revision/删除状态/大小），不得解码明文——`KeyPairEntry.PrivateKey` 绝不进任何渲染文本（ADR-0005 红线）；`cmd/sync.go` 的 `isConfigSourceKind` 扩入两个新 kind，冲突报告的 alias+revision 对照提示覆盖 SSH 资产
- `.agents/skills/senv-cli/SKILL.md`：随代码变更更新同步范围说明（AGENTS.md「同一变更」约束）

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- per-kind 同步开关（ADR-0020 rejected，后加开关向后兼容）
- keypair 拆分同步（公钥出机、私钥不出机）
- senv-server 代码改动：无（白名单来自 client/server 共享的 syncschema 包）。但发布顺序约束成立：senv-server 镜像必须先重建上线，否则新 client 携带 ssh 条目的整批 push 会被旧 server 事务前校验拒绝（连带 env/text）；镜像构建发布按 `docs/senv-server.md` 流程执行，属运维动作不在本任务代码范围
- TUI 改动（`reloadAllTabs` 通用机制已覆盖 ssh tab）
- `~/.ssh/senv/` 落盘件的同步（本机状态，ADR-0001 不变）
- MCP/LLM 同步通道语义改动（ADR-0019 范围）

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] ssh_host/ssh_keypair 档案随同步通道分发：collect 覆盖 hosts/keypairs 目录、landing 写回原路径，单测覆盖收集→落地 roundtrip
- [x] syncschema 白名单为九 kind，两个新 kind 身份校验=别名（grp 空），未知 kind 拒绝路径不回归
- [x] host export 对本机 vault 缺失的 keypair 引用逐条 warning 且照常写出
- [x] 冲突报告对两个新 kind 仅渲染元数据，测试断言私钥明文不出现在渲染输出
- [x] `.agents/skills/senv-cli/SKILL.md` 已更新（同步范围含 SSH 资产、新机器凭口令取用全量 SSH 资产）
- [x] 全仓 `go test ./...` 通过；`go run . --help`、`go run . ssh --help` 语法正常

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `ssh-sync-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/ssh-sync-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

- 2026-09-13 propose：拆 2 个子 change（channel/export-warning），`openspec validate --strict` 三 change 全过。
- 2026-09-13 apply：channel 10/10、export-warning 7/7（含 review 修复节 3 项）；实现期间发现 server 侧经 `internal/server/store` 共享 syncschema 白名单（Round 2「server 免升级」结论被推翻），已回改 ADR-0020 Consequences 与本 proposal Non-goals——发布顺序必须 server 镜像先行。
- 2026-09-13 review（docs/reviews/2026-09-13/ssh-sync）：P0=0 P1=1 P3=10。P1（carry-over help.go 窄终端空内容体）已修+回归测试；host.go 7 条 P3 全修；help.go 其余 3 条 P3（用户 WIP 折行溢出）记录不修。
- 2026-09-13 收尾：`make check` 全绿（fmt+vet+lint+`go test -race ./...`）；`go run . --help`、`go run . ssh --help`、`go run . ssh host export --help` 语法正常。
