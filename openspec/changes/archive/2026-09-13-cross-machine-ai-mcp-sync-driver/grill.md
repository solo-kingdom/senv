# Grill：cross-machine-ai-mcp-sync

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 同步范围 | 人工添加进 vault 的"配置源"数据应当跨机同步；本机状态（Coding Agent 切换指针 / MCP export ledger / agent 配置文件）不同步 | 与 CONTEXT.md "本地状态"分组及 ADR-0003 / 0007 / 0012 的边界一致；"所有"不是字面 | settled |
| D2 | LLM Provider 档案与 MCP Server 档案本体 | 加入 sync 通道 | 它们是"配置源"，与 env/text 同形态（SSH-style 加密 blob），不在内的代价是每台机重配 | settled |
| D3 | syncschema kind 命名 | 新增 `llm_provider` 与 `mcp_server` 两个 kind | 跟 `LLMProviderDirName` / `MCPServerDirName` 常量对齐，grep 友好；不与 ssh keypair 撞名 | settled |
| D4 | 凭据本体是否出机 | 不出。LLM profile 只存 `credential_ref`（指向 vault text group `llm-keys`），MCP secret 以 `{{env:...}}` / `{{text:...}}` 模板随 profile 同步，由 export 端在 export 时解析 | `text` kind 本来就同步，credential_ref 是引用；模板不是明文 | settled |
| D5 | 冲突策略 | 沿用现有 LWW + revision；但 `llm_provider` / `mcp_server` 冲突时多打一条 warning 到 stderr / TUI audit | 不强制裁决流程，与"配置源"语义匹配（人工源数据并发更可能是有意义的双改） | settled |
| D6 | 首次 bootstrap 姿态 | 直接随 sync 拉，不加 opt-in 闸门；保留 `--accept-remote` 重建 | "配置源应当同步"已把"配置源先到本机"定为默认值；opt-in 与该原则抵触 | settled |
| D7 | 凭据引用跨机解析失败 | `senv ai switch` fail-closed + 诊断"缺失 text:llm-keys/<alias>"；`senv mcp export` 仍写入字面量（保持向后兼容）+ 把缺失 env/text 名列入 warning | fail-closed 防绕过 A 机对凭据来源的意图；MCP 维度沿用既有 export 行为，只多打 warning | settled |
| D8 | TUI 审计面 | 复用现有 audit 面板加 "since last pull" 子视图，不开新面板 | 现有 audit 面板已就位（提交 4ddbabf / 6f17d92），加一类字段即可并入 | settled |
| D9 | ssh_host / ssh_keypair 范围 | 本 change 不纳入；ADR 显式记入"已识别但延后"项 | 用户原命题只点了 AI + MCP；纳入会让本 change 体积翻倍、review 难度上升 | settled |
| D10 | driver 归属与产出 | 新建 driver `cross-machine-ai-mcp-sync-driver`；决策进 driver/grill.md，ADR 候选 `sync-ai-mcp-source-of-truth` 由 propose 阶段晋升进 `docs/adr/`（编号落地时取下一空位）；`CONTEXT.md` 在 apply 阶段同步更新 | 决策树已长成具体子任务（syncschema / entryLocation / CLI / TUI / ADR / 文档），需 OpenSpec 跟踪 | settled |
| D11 | ADR 编号 | 不写死编号，改"落地时 `docs/adr/` 扫描取下一空位"措辞（2026-09-12 复核为 0019） | 0018 已被 server-cache-out-of-band-invalidation 占用；硬编号已过期一次，且仓内 0001 已有双文件先例 | settled |
| D12 | `.openspec.yaml` 补建 | 按 taskflow 模板补 `skip_specs: true` 脚手架 | taskflow-new 第 3 步被跳过、文件从未存在；不补则 propose 会搭 spec 增量骨架 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| 配置源 | 人工添加进 vault、可被 senv 跨机分发的数据（env / text / llm_provider / mcp_server / ssh_host / ssh_keypair 等事实型加密 blob） | 新造；与"本机状态"相对。本 change 之前隐含在 env/text 里，未做显式定义 |
| 本机状态（同步边界语境） | 仅在单一 client 上有意义、刻意不同步的状态；典型为 Coding Agent 切换指针、MCP export ledger、本机 agent 配置文件 | 与 CONTEXT.md "本地状态"分组（解锁缓存 / 持久会话 / 工作副本）是**同名不同物**：那里指本机缓存与副本，这里指同步边界的"per-machine state"。本次新增定义需在 CONTEXT.md 显式区分 |
| 人工添加 | 由 `senv ai provider add` / `senv mcp add` / `senv env set` / `senv text add` 等命令产生、不是同步通道或派生计算得来的条目 | 沿用 senv 文档对"用户操作"的既有指代；不引入新词 |
| 漂移（Drift） | senv 侧事实源与 agent 侧派生产物不再一致、且 senv 不回读 agent 配置去自动纠正的状态 | 沿用 ADR-0007 的 MCP 导出漂移语义；本次继续保留 |

## ADR 候选

<!-- 仅记录满足"难逆转 + 后人费解 + 真实取舍"三门槛的；由 design.md 吸收或随 change 归档晋升 -->

- [ ] adr-sync-ai-mcp-source-of-truth: syncschema 增补 `llm_provider` / `mcp_server`，与 env/text 走同一通道；本机派生态（Coding Agent 切换指针 / MCP export ledger / agent 配置文件）仍不同步（出处：D1/D2/D3/D4/D5/D9）→ 待 propose 阶段晋升进 `docs/adr/`（编号按落地时扫描取下一空位，2026-09-12 复核为 0019）

## 未决问题

无

## 验证记录

- 2026-09-12 grill 第二轮：环境复核发现 ADR 0018 撞号与 `.openspec.yaml` 缺失，D11/D12 settled；frontier 清空，grill 收敛，进入 openspec-propose。