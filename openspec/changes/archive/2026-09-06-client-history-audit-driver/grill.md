# Grill：client-history-audit

第一轮 frontier 一次性抛出 7 题（Q1–Q7，均带推荐答案），用户整体确认「按推荐的来」。以下全部 settled。

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 注册形态 | 一次性注册码：管理员 `senv-server admin create-registration --user <u> [--expires 24h]` 生成一次性 code；client 端 `senv server register --address ... --code XXX --name <设备名>`，server 创建 client 记录并发放该 client 专属凭证（明文仅展示一次） | 离线可操作、无需 Web UI；一次性+过期使泄露窗口最小；审批队列 UX 重，开放自注册不适合密钥工具 | settled |
| D2 | client 实体与屏蔽语义（server 侧闭环） | 新增 `clients` 表（id、user_id、name、status、created_at、last_seen_at），凭证从挂 user 改为挂 client 下；屏蔽 = status 置 blocked（可解封），该 client 名下凭证全部失效但记录保留；revoke 语义不变；被屏蔽 client 的 server 数据不动；粒度 = 单台设备，不影响同 user 其他 client | 设备级身份是屏蔽语义的载体；block（可逆、绑定实体）与 revoke（不可逆、绑定凭证）分离；零知识下数据属于 user 的 vault 不属于设备 | settled |
| D3 | client 被屏蔽后的行为（client 侧闭环） | server 对被屏蔽 client 返回 403 + 机器可读码 `client_blocked`（区别于 401 凭证无效）；client 任一 API 调用收到 `client_blocked`：自动清理解锁缓存（SessionCache）、保留本地加密数据、提示「已被服务器屏蔽，已清除本机解锁缓存；本地加密数据保留」并附重新注册指引，退出非零；后续每个命令重复提示直至解封或重新注册 | client 必须能区分「被屏蔽」与「凭证坏」；自动清缓存是需求原文要求；本地密文数据无 server 密钥参与，保留无害 | settled |
| D4 | 配置历史版本粒度与能力 | 条目级：每个条目在 server 保留最近 N 个密文历史（复用现有 revision 单调号，新表 `entries_history`）；默认 3 版，server 侧 `--history-retain N` 可配；**查看 + 单条目恢复**都做（恢复 = 把历史值写回当前值，复用现有推送流程，含误删条目找回）；cli + tui 均可查看；仅 server provider | 条目级成本远低于整库快照且覆盖「改坏/误删想找回」主诉求；git 模式已有完整 git 历史，不重复建设 | settled |
| D5 | 操作审计存储与可见范围 | 仅本机：扩展现有 `~/.log/senv/audit.log`（JSON-lines，已有 session/MCP 事件），增加业务操作事件（env/text/config 增删改、install/uninstall、sync、冲突解决）；记录「操作 + 目标 + 日期时间 + 结果」，**不记值**；`senv audit` CLI + TUI 查看；跨机同步为后续可选升级 | 闭环快、与现有 audit.go 同路；不记值避免敏感值落审计文件；同步审计复杂度高（写入时机、条目放大）不值得 v1 承担 | settled |
| D6 | 系统日志粒度与查看入口 | 每个 API 请求记一行：时间、IP、method+path、client/user、结果（OK / AUTH-FAILED / BLOCKED / RATE-LIMITED），push/pull 天然即「同步」事件，认证失败附原因；查看入口 `senv-server admin logs`（--user --client --since --until --outcome --limit），仅服务器管理员；v1 只增不自动删，提供 `admin logs prune --before <date>` 手动清理；不给 client 开查询 API | Bearer 模式无登录端点，「登录」= 单次请求认证；安全日志属管理员，client 侧可看的已由 D5 覆盖 | settled |
| D7 | provider 范围与非目标 | 全部按 server 模式为主设计；git 模式下：历史版本 = git 本身能力（不做）、屏蔽/系统日志 = 无 server（N/A）、操作审计 = D5 本机文件天然可用 | 避免双通道重复建设 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| Client（客户端设备） | 注册到 server 的一台设备/一份安装，属于某 user，有自己的名字与专属凭证，可被屏蔽 | 新造（现状凭证挂 user 下、无设备概念） |
| 屏蔽（Block） | 对单个 client 的**可逆**禁止状态：凭证即刻失效、请求被拒，client 记录与 vault 数据保留 | 新造；明确区别于 revoke |
| 吊销（Revoke） | 单份凭证的不可逆作废 | 沿用现有 `admin revoke-token` 语义 |
| 解锁缓存（Session Cache） | client 本地口令派生密钥缓存（SessionCache），与 server 无关；被屏蔽时清除，本地加密工作副本保留 | 修正：用户原话「清理 session（缓存数据可以保留）」中的 session 即指它，与「server 会话」划清界限 |
| 登录事件（Auth Event） | 一次 API 请求的认证结果（成功/失败原因）；无交互式登录会话 | 新造（现状无登录端点） |
| 操作审计（Operation Audit） | client 侧本机业务操作流水，不含值，仅本机查看 | 新造（现有 audit.log 只记 session/MCP 事件） |
| 系统日志（Access Log） | server 侧每请求安全事件流水，存数据库，仅管理员可查 | 新造 |
| 配置历史版本（Entry History） | server 为单条目保留的最近 N 个密文历史（默认 3），可回看可恢复 | 新造（现状仅 revision 单调号、不存旧值） |

## ADR 候选

- [ ] adr-client-device-identity: 认证主体从 user 级凭证改为设备级 client 实体；屏蔽为可逆设备状态、与凭证吊销分离；被屏蔽以 403 + `client_blocked` 表达（出处：D2/D3）
- [ ] adr-entry-level-history: 历史版本采用条目级密文历史而非整库快照，恢复复用现有推送流程（出处：D4）
- [ ] adr-local-operation-audit: 操作审计仅本机存储、不随 vault 同步、不记值（出处：D5）

## 未决问题

无
