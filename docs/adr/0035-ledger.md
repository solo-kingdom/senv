# 台账（Ledger）：不可变记录、服务端归因与入口来源策略

台账是「实体 + 只追加的操作记录」型配置源：设备、基础设施、服务、agent 端点各一份，记录发生了什么、谁做的、怎么验证、怎么回滚。今天这类数据以 Markdown + git 承载（单一私有仓内约 146 个实体、约 1000 条操作记录、近期约 250 条/月、并发写者 ≥10 个 agent 身份），缺陷是结构性的：无锁（靠人工合并，四个月内出现十余次双侧冲突标记）、回滚只靠 git、变更者只能自报、派生索引与看板手工同步、明文落盘。senv 已有加密 blob、分组、单调 revision、逐条乐观锁、条目历史、访问日志、设备 client 与告警，但同步层对真实冲突**只检测不合并**，推送单批上限 1000 条，历史默认只留 3 版且**不记操作者**，同一条目重复推送会消耗历史，vault metadata 被无条件覆盖且无历史。本 ADR 决定台账的数据形态与访问面，并给出回滚、排查与入口来源策略的设计；实现拆为独立变更（见 Consequences）。

## 决策

### 1. 内容组织

1. **新增三个同步 kind：`ledger`（实体头）、`ledger_log`（记录）、`ledger_meta`（分组说明）。** 身份矩阵沿用 text 家族：`ledger` / `ledger_log` 要求 grp 与 key 均为安全单路径段；`ledger_meta` 要求 grp 非空、key 为空。不复用 `text`：那等于把冲突、回滚、归因三个问题原样搬进 vault，且回滚深度反而比 git 浅（3 版 vs 全历史）；不在 server 侧建结构化表：那会打破零知识不变式（ADR-0019/0020），并让 git 通道不可用。
2. **实体身份 = grp（归属桶）+ key（`<type>.<id>`）。** 归属桶是扁平的归属标签（如按 ssh config 来源的三类，加一个默认桶），不表达层级（ADR-0026）。`type` 是开放的小写枚举（`device` / `infra` / `service` / `agent` / `container`，后续可扩）；`id` 用**别名**而非主机名（身份对服务端可见，正文不可见），禁 `@`。示例：`device.<alias>`、`service.<name>`、`agent.<host>.<kind>`。
3. **记录身份 = 同 grp + key = `<实体 key>@<YYYYMMDDTHHMMSSZ>-<4hex>`。** 按前缀枚举即某实体的全部记录，按字典序即时间序，无需任何索引条目；4 位随机后缀保证同秒并发追加不撞 key（撞了由 base 0 冲突暴露，客户端换后缀重试）。
4. **实体头载荷**（加密 JSON）：`schema`、`description`（一句话用途，进检索面）、`tags`、有序 `fields`（名称/值保序，允许占位符）、`status`（最近 1–2 条观测，含 `at` 与自报 `by`）、`links`（可选：关联 Host 档案、部署目录、其他实体）、`created_at` / `updated_at`。`status` 不进记录：巡检频率远高于操作，入记录会冲垮时间线，且今日语义本就是「保留最近 1–2 条」。
5. **记录载荷**：`schema`、`entity{type,id}`、`at`（事件时间，可回填历史）、`kind`（`op` / `note` / `correction`）、`summary`（≤200 字节，列表与时间线展示）、`body`（Markdown；推荐沿用「做了什么 / 验证 / 回滚 / 关联」标签以保持阅读习惯）、`by{host,user,agent,client?}`（客户端自报）、`supersedes[]`（更正指向被更正记录）。
6. **记录不可变**：没有更新接口。写错了追加 `correction`；删除只有显式 CLI（确认 + `--force`），MCP 不提供。单条记录 body ≤64KiB、实体头 ≤256KiB（均低于 512KiB 密文上限）；台账内**不放密钥值**（沿用「只记指纹或引用」），不做 `{{text:…}}` 自动解析。
7. **不引入任何共享可变索引条目。** 现有 `config_index` / `skill_index` 的形态（整索引一个 grp/key 均空的条目、无字段级合并）在两台设备各改一处时必然冲突，且 `skill_index.json` 以明文入库——台账的摘要比 skill 描述更敏感，不复用。列表 = 目录扫描（本地）或增量拉取（远端）；跨实体时间线 = 按 key 内时间戳排序后按需解密。
8. **输出面：`senv ledger list --json` 与 `senv ledger export --path <dir>`**（按实体渲染 Markdown 镜像，0600）。消费侧的索引表、看板、仪表盘数据由各自生成器从 `--json` 产出，不再手工同步。
9. **访问面**：CLI 全量（`list` / `get` / `log` / `timeline` / `append` / `edit`（单字段）/ `set-status` / `history` / `delete-record` / `export` / `import`）；MCP 只读 + `append` + `set-status` + 单字段 `edit`，**无删除**（失控 agent 的最大破坏半径 = 多写几条可更正的记录）；TUI 一等（Ledger Tab：类型/分组的实体列表 + 详情 + 追加表单），对齐「CLI 与 TUI 两面同评」。
10. **迁移**（实现任务）：`senv ledger import` 按标题前缀解析现有 Markdown（`基本信息` / `最近状态` / `操作记录` 及带括号变体），记录日期 → `at`，「操作人 / Agent」→ `by.agent`，其余正文原样入 `body`；先 `--dry-run`，再按 ≤900 条/批导入并分批 `sync`；源仓保留每实体 stub（原有链接不破），维护 skill 改指 `senv ledger`。
11. **术语**：CONTEXT.md 增补「台账 / 实体 / 记录 / 更正 / 来源绑定」；现有非正式用法「本机台账」（`mcp-exports.json`）改为「导出登记」，避免撞名。

### 2. 回滚

1. **记录不可变让「回滚单条操作」退化为「追加更正」**：无需历史深度，也不会与他人并发冲突；`supersedes` 保留撤销链。删除是显式动作，产生 tombstone，最后版本留在条目历史里。
2. **修正前像写入条件**：只有条目内容真的变化才写 `entries_history`；相同密文重推、tombstone 重推不消耗版本。今天「重复推 tombstone 即可把一个条目真正的最后一版挤出历史」是回滚能力的漏洞：调用方只需 token、不需要 vault 口令。
3. **历史行增 `replaced_at` 与操作者列**：`replaced_at = now()`（变更时刻），`actor_client_id` / `actor_user_id` 由 push 事务内的 token 推出；history API 返回。现状 `created_at` 是**前像写入时间**，vault 级历史查询按它排序，不能当「最近变更时间线」。
4. **部署值**：`--history-retain` 建议 ≥10（纯配置，存储量 = 条目数 × N × 密文大小，可忽略）。
5. **批量恢复**：`senv ledger restore --since <ts> [--client <name>]` 按历史逐条恢复（每条产生新 revision，动作本身可审计）；替代手工逐条 `senv history --restore`。
6. **metadata 兜底**：`vault_metadata` 增历史（保留最近 N 个 blob）与客户端 revision 校验。metadata 是唯一「覆盖即不可用」的对象（salt / passwordKey），今天无条件覆盖、无历史，恢复只能靠库级 dump 或尚未拉取的客户端本地副本。
7. **数据底线**：库级 dump 频率由日级提到小时级（senv 库在 MB 级），RPO 24h → 1h；git 模式下 git 历史是回滚源，`ledger export` 提供明文冷备。
8. **已知边界**：误把密钥写进台账 → 删除后密文仍留在历史中直到被后续版本挤出；处置口径与 senv 其它面一致（轮换该密钥，不建议依赖删除）。历史裁剪目前由 serve 角色执行；建议移到 admin cron，使 serve 角色对 `entries_history` 只 INSERT+SELECT，历史对「被攻陷的服务进程」也保持不可改写。

### 3. 排查

1. **三层归因**：① 记录内 `by`（随密文走、git 模式也有、可伪造，用于「优先怀疑范围」）；② 服务端认定的历史操作者（决定 2.3，token → client/user，不可由客户端伪造）；③ 请求日志（来源 IP、结果、push 计数）。三层都只含标识，不含值。
2. **前置：逐设备 client**。共享用户级 token 的机器既没有服务端归因、也无法被单独屏蔽；上线台账访问面之前，先把每台设备注册为独立 client（已有一次性注册码流程与屏蔽/解封命令）。
3. **日志补真**：`access_log` 记录真实结果（400/409/413/5xx 不再一律记 `OK`），push 记录 `n_entries` / `n_deleted`；`admin logs` 增按 IP 过滤。破坏性动作因此可以直接从日志读出「哪台设备在何时提交了多少条删除」。
4. **查询面**：`senv ledger log <实体> [--since --by]`、`senv ledger timeline [--type --group --by]`、`senv ledger history <实体>`（服务端认定的操作者 + 变更时间）、`senv audit --type op_ledger`（本机）、`admin logs --client`（服务端）。MCP 审计的 target 从「工具名」补到「实体 + 记录标识」（仍不含值）。
5. **冲突诊断**：追加天然无冲突；实体头的并发修改走既有冲突解决器并在本机审计留 `op_conflict`；git 模式回滚/合并冲突仍按既有「手工解决、不 force push」语义。台账不再需要 `<!-- merged by -->` 式人工合并。
6. **漂移诊断**：派生视图（索引表、看板）由生成器从 `list --json` 重建；读取前按读路径自动拉取（节流窗口 30s），需要最新用 `--refresh`；记录与状态条带 `at` / `observed_at`，「台账 vs 现场」的核对仍是巡检 skill 的职责，不由存储层代劳。
7. **取证 runbook**（写进 agent 使用文档）：识别（时间线 / 告警）→ 止损（`block-client` / `revoke-token` / 边缘白名单收口）→ 定量（历史 + 请求日志 + push 计数）→ 恢复（批量 restore）→ 复盘（把事件本身追加为一条记录）。

### 4. IP 白名单评估

1. **它针对的「极端情况」分四类，白名单只对两类有效**：

| 场景 | 边缘来源白名单 | 按 client 来源绑定 | 结论 |
|---|---|---|---|
| 盗取的 token 从可信网络之外使用 | 有效（404 在认证之前） | 有效（来源不符即拒） | 值得 |
| 盗取的 token 从可信网络之内使用（共享出口 NAT、同 WG 其它设备） | 无效 | 有效（按设备收窄） | 只能靠后者 |
| 合法设备上的失控 agent（已解锁 session） | 无效 | 无效 | 靠工具面与删除防护 |
| 公网扫描、预认证漏洞面 | 有效（不进应用层） | 不涉及 | 值得 |

2. **全局来源白名单放边缘，不进 senv**：反代（`remote_ip` 匹配器）/ 云安全组 / 仅 WG，零代码、分钟级回滚，覆盖上表第 1、4 行。部署侧已按此方向另案推进，本 ADR 不重复其执行细节。
3. **senv-server 内建能力限定为两点**：① `--allow-cidr`（静态全局 CIDR，默认关闭，fail-closed，loopback 与私网恒放行，拒绝记 `IP-DENIED` 并告警）——用于**直连部署**或反代配置回退时的防纵深；② **按 client 的来源绑定**：`clients.allow_cidrs` + `off` / `audit` / `enforce` 三档，admin CLI 写库、沿既有 LISTEN/NOTIFY 失效广播即时生效（无需重启、无需走 HTTP 面），audit 档只记录「本该拒绝」。后者是 senv 独有价值：token 已知对应哪台设备，来源不符时即使边缘白名单放行也能拒。
4. **不建议**：把 IP 名单当主控（动态家宽三个月三个出口、办公室共用 NAT、曾出现回收地址被他人使用）；给动态出口做静态白名单；用 Caddy 匹配器承担**单设备**粒度；在有不可信私网对端时采信 `X-Real-IP`/`X-Forwarded-For`（`resolveRemoteIP` 只对 loopback/私网对端采信，代码注释已声明「同一私网内其它主机也视为可信代理」；Caddy 默认丢弃客户端自带的 XFF，单层反代拓扑下归因成立，但 `header_up X-Real-IP` 仍应由部署侧显式设置并纳入检查）。
5. **锁定与恢复**：边缘——删除匹配器 + reload（独立于 HTTP 策略的 ssh 通道）；应用层——`--allow-cidr` 空 = 关闭（默认）、loopback 恒放行、admin CLI 走数据库不经 HTTP、按 client 的绑定可由 admin 清空。所有拒绝都要进日志与告警，否则「合法设备被白名单挡掉」会表现为静默同步失败。
6. **实施顺序**：边缘白名单（部署侧）→ 回滚与归因相关修正（本 ADR 第 2、3 节，P0）→ 逐设备 client（前置）→ 按 client 来源绑定（audit 两周 → 逐台 enforce）→ `--allow-cidr`（可选）。IP 白名单排在回滚与归因之后：被攻陷时可以恢复、可以查清，比挡住入口更决定损失上限。

## Considered Options

- **复用 `text`，用约定承载追加语义**：零代码，但并发追加仍是整条冲突、历史 3 版即上限、无归因字段——把问题原样搬进加密仓；否。
- **一实体一条目（头 + 全部记录一个 blob）**：读取最省（1 次解密），但同实体并发追加必冲突，主体积会撞 384KB 明文上限（现有最大实体已 126KB 且按月增长）；否。
- **按时间分桶（实体 × 月）**：只把冲突概率降低一个数量级，不消除；否。
- **server 侧结构化表 / SQL 取证**：破零知识、git 通道不可用、迁移与回滚都更重；否。
- **共享明文索引条目**（仿 `skill_index.json`）：冲突热点 + 摘要明文入库；否。
- **记录可编辑（update 语义）**：需要 3 路合并与更深的版本保留，且与「只追加」的既有纪律相悖；否。
- **`status` 入记录**：时间线被巡检淹没；否。
- **MCP 提供删除**：失控 agent 的最大破坏半径从「多写几条」升级为「抹掉历史」；否。
- **仅 WG 暴露、不做公网入口**：最安全，但离隧道即失去同步，且隧道在部分客户端有静默失效先例；保留为可选收紧项，不作默认。
- **IP 白名单作为主控制**：见第 4 节，覆盖不了三类威胁中的两类；否。

## Consequences

- **实现触点**（新 kind 的既有清单）：`internal/storage/<kind>.go`、`internal/syncschema/schema.go`（3 个 kind 常量 + 身份分支）、`internal/provider/server_state.go`（别名 / `entryLocation` / 收集；**漏加 `entryLocation` 分支会在同步时命中 `panic("syncschema accepted unknown kind")` 失守防线**，实现时按 kind 逐个补齐并加单测）、`consistency.go`、`rekey.go`、`rekey_manifest.go`、`doctor.go`、`auth.go`、`history.go`、`internal/session/types.go`（`AuditOpLedger*`）、`internal/search/gather.go`（实体头进检索面）、`cmd/ledger*.go`、`internal/tui/ledger_tab.go` 与 Tab 注册、MCP 工具与 catalogue / guard 测试、conflict render（头可合并，记录不参与合并）。
- **发布顺序与客户端门槛**：server 先升级（syncschema 白名单在 server 二进制里），再让客户端产出新 kind；**客户端也必须升级到识别新 kind 的版本——拉取批次里出现未知 kind 时客户端整批拒绝、同步失败**（现场先例：旧客户端遇到 `skill` kind 报 `unknown kind`，多台机器同步中断）。台账 kinds 上线前必须先完成客户端滚动；错误文案应点名「升级客户端」而不是只报 unknown kind。
- **前置变更（不并入台账实现，独立 ADR/变更）**：① 客户端 push 分批（单批上限 1000 与「全部 dirty 一次推」的组合会让条目总数过千后的 rekey / 批量导入整批被拒）；② 前像仅变化时写；③ history 增 `replaced_at` 与操作者；④ `access_log` 真实结果 + push 计数 + 按 IP 过滤；⑤ 破坏性写防护（tombstone 上限 + `mass_delete` 告警）；⑥ metadata 历史 + revision 校验；⑦ serve 角色对 `entries_history` 只读、裁剪移交 admin cron；⑧ 全员客户端升到识别台账 kind 的版本（滚动窗口与回退方案）。
- **文档**：`.agents/skills/senv-cli/SKILL.md` 增补 `senv ledger` 与取证 runbook；`docs/senv-server.md` 增补来源策略 flag、`header_up X-Real-IP` 检查项与「服务端可见字段」清单（现缺 group/key 与两个明文索引 kind）；CONTEXT.md 增补术语。
- **风险**：实体头仍是并发写点（低频率、有冲突检测与历史）；记录数按月增长（~250/月 → 稳态万级），列表与时间线依赖目录扫描与 key 内时间戳，不建索引；git 模式下无服务端归因，只有记录自报；误写密钥需轮换而非依赖删除。

## Status

accepted（2026-11-23 设计定稿；实现拆分为独立变更，见 Consequences。同日修订一次：按现场证据更正客户端兼容语义——旧客户端拉取未知 kind 会整批拒绝，需要先滚动客户端）
