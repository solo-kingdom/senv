# Grill：add-mcp-server-export

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 需求语义 | 新增**一等数据类型的 MCP Server 档案** + 导出，不是扩展现有 `senv mcp install` | 后者已覆盖「多 agent + user 全局」，照字面读不构成新特性 | settled |
| D2 | 承载方式 | 新数据类型 `mcp`，不复用 `config` 条目 | `config install` 是字节复制，导出需要按 agent 格式做结构化 merge；让 `config` 承担 merge 会污染其「内容相同即 skip」契约 | settled |
| D3 | 档案字段 | 别名唯一标识 + 传输类型 + 该传输的字段原样保存（V1 仅 stdio：`command`/`args`/`env`） | senv 不解析、不代理、不补协议 | settled |
| D4 | 导出目标集合 | 复用 `senv mcp install` 的 7-agent 注册表（claude-code/claude-desktop/cursor/codex/zcode/kimi/pi） | claude-desktop/cursor 装不了 LLM provider 但装得了第三方 MCP server；两张表用途不同，本轮不统一 | settled |
| D5 | 作用域语义 | 默认 user 级（= 全局配置）；`--scope project` 沿用现状（仅 cursor 生效），不新增 project 支持 | 把「全局 = user 级配置文件」一次定死 | settled |
| D6 | 敏感值策略 | 值加密存 vault；导出按 ADR-0002 先例明文写入 agent 配置（0600），计划页显式标注；值支持 `{{env:...}}`/`{{text:...}}` 引用 | 引用只解决「值不重复存」，不改变导出=明文落盘 | settled |
| D7 | 冲突与覆盖 | senv 写过的条目可直接覆盖；同名非 senv 条目默认拒绝，`--force` 放行；merge 保留无关键 + 备份 | 复用 `mcp install` 的 merge-and-write 心智 | settled |
| D8 | 同步与状态 | 档案进 vault、多端同步；导出台账是本机状态，**不进 vault** | 与 ADR-0003 同构：agent 配置是本机的，同步台账会与实况脱节 | settled |
| D9 | 交付面与命名 | CLI 先行（`add`/`list`/`remove`/`export`/`unexport`）+ 只读 MCP 工具；`install` 保持「装 senv 自身 server」语义；不自建内置档案；TUI 延后 | ADR-0005 刚把 `mcp install` 留在 CLI，别本轮破例 | settled |
| D10 | 「谁写的」判定 | **本机台账** `~/.config/senv/mcp-exports.json`（agent → 别名 → 内容指纹）；指纹不符即漂移 | 修正 D8 早期的「不记任何台账」：无台账无法区分 senv 写入 / 漂移 / 外部同名，且本机台账正是 ADR-0003 的先例 | settled |
| D11 | 传输支持范围 | V1 仅 stdio | 各 agent 对 http/sse 的键名不统一且无权威公开文档，写错即静默坏配置；作为逃生舱延后 | settled |
| D12 | 导出粒度 | 必须显式给目标（`--agent a,b` / `--all`），不设「默认全部」；一律先计划后执行；`--print`/`--dry-run` 只输出不落盘 | 对齐 config-install 的 plan→confirm 契约 | settled |
| D13 | 撤回 | `senv mcp unexport`，语义对齐 config-uninstall（内容一致才删，被改过需确认）；档案 `remove` 不自动 unexport | 本机状态跨机不同步，自动删会误伤 | settled |
| D14 | MCP 工具面 | 仅 `mcp_server_list`，只返回 alias/传输类型/描述，不返回值；不给 get、不给写侧工具 | 与 `sshHostView`（无承载私钥字段）、`llmProviderView`（只给 `credential_ref`）同构 | settled |
| D15 | 档案形态附加项 | 不要 group、不要 `enabled` | group 是 env 的「多环境」语义；`enabled` 是 agent 侧概念 | settled |
| D16 | 备份命名 | 沿用 `<file>.bak`（与 `mcp install` 同族），不动 config 侧的 `.senv-backup-<时间戳>` | 同族一致优先，避免改动既有行为 | settled |
| D17 | `command` 路径归一 | 不归一，原样写；仅 senv 自身二进制继续绝对化 | `npx`/`uvx` 依赖 agent 的 PATH 解析，绝对化会把运行时版本钉死进配置 | settled |
| D18 | agent 特有键透传 | V1 不透传，只落跨 agent 可移植子集 | 透传会让 agent 矩阵渗进 vault schema（ADR-0004/0006 反复挡的同一件事） | settled |
| D19 | 多 agent 失败语义 | 不做整体回滚，逐条独立成败并报告；台账只记成功项 | 跨多文件事务回滚会制造更糟的「半回滚」状态 | settled |
| D20 | 明文确认强度 | 计划页标注明文条目与目标路径 → 一次确认（对齐 config-install） | 不加额外 `--yes`/二次确认 | settled |
| D21 | 交付链路 | 单个 change，不建 driver | spec 面是一条连贯线，无需 driver 编排 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| MCP Server 档案 | 一份第三方 MCP 服务器接入定义：别名唯一标识，含传输类型与启动要素；存于 vault | 新造；已入 `CONTEXT.md` |
| 导出（Export） | 把档案合并写入目标 Coding Agent 全局配置的动作；agent 配置是派生产物 | 新造；与「安装」区分；已入 `CONTEXT.md` |
| MCP 安装（Install） | 把 senv 自身 MCP server（`senv mcp serve`）写入 agent 配置 | 修正常见误用（拿它指导出用户档案）；已入 `CONTEXT.md` |
| 全局配置（Global Config） | Coding Agent 的 user 级配置文件，作用于当前用户所有项目 | 新造；_Avoid_ 用户配置（易与 server 侧 user 混淆）；已入 `CONTEXT.md` |

## ADR 候选

- [x] adr-mcp-export-source-of-truth: vault 是 MCP Server 档案唯一事实源，导出状态是本机状态，指纹不符即漂移（出处：D8/D10）→ 已晋升 `docs/adr/0007-mcp-export-source-of-truth.md`
- [x] adr-plaintext-mcp-secrets-on-export: 明文落盘沿用 ADR-0002 的取舍（出处：D6）→ 已晋升 `docs/adr/0008-plaintext-mcp-secrets-on-export.md`
- [ ] ~~adr-mcp-stdio-only~~: 未采纳——「先只做 stdio」属容易逆转的范围决策，不满足 ADR 门槛（D11）；改记为本 change 的 Non-goal

## 未决问题

无
