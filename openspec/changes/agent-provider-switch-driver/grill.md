# Grill：agent-provider-switch

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 新功能中 "provider" 的 canonical 术语 | **LLM Provider**；agent 侧叫 Coding Agent | 与既有同步后端 provider（`internal/provider`、`senv-server-provider-*`）冲突，必须区分；术语已入 `CONTEXT.md` | settled |
| D2 | 首期支持的 coding agent 名单 | claude-code、codex、zcode、kimi、pi、**opencode**；cursor best-effort；claude-desktop 不做 | 复用 `senv mcp install` registry 与 JSON/TOML 写回先例；opencode 为用户点名新增 | settled |
| D3 | 切换机制 | 改写 agent 自身配置文件（A）；环境变量注入不进首期 | 各 agent env 变量名不统一且部分不读 shell env；复用 mcp install 的 merge-and-write 模式 | settled |
| D4 | API key 存放 | 存 senv vault（env/text entry），provider 档案只存引用；切换时解密写入 agent 配置 | senv 核心价值是加密存储；照搬 SSH KeyPair「vault 存私密 + 落盘」模式 | settled |
| D5 | models.dev 数据获取与刷新 | 显式 refresh 拉取落盘缓存；add 时自动检查缓存新鲜度并补最新模型；自定义模型手动添加、不依赖目录 | 离线可用、行为可预期；`https://models.dev/api.json` 已验证可达 | settled |
| D6 | 命令命名空间 | **`senv ai`**（`senv ai provider add/list/...`、`senv ai refresh`、`senv ai switch`、`senv ai status`） | 用户选定 B；`senv provider` 已被同步后端占用 | settled |
| D7 | 切换状态模型 | senv 存每 agent 单向指针 `(provider, model)`；切换 = 更新指针 + 写 agent 配置；status 不回读 agent 配置；一个 agent 一个指针，不做多 profile | 各 agent 配置格式异构，回读解析易碎；复用 mcp install 单向写入心智 | settled |
| D8 | 切换作用域 | 首期仅 user 级 | project 级牵扯各 agent project config 差异，留后续子 change | settled |
| D9 | 首期交互面 | **CLI + TUI + MCP 全上** | 用户选定 C；能力边界见 D10 | settled |
| D10 | TUI / MCP 首期能力边界 | TUI 浏览 **+ 切换操作**（选 agent → provider → 模型，即时生效）；MCP **只读查询**（provider 列表、当前指向） | 切换是核心动作，TUI 是自然日常入口；MCP 允许 agent 切自身 provider 有安全疑虑 | settled |
| D11 | agent 指针的数据驻留 | 当前指向是**本机状态**，不进 vault 不同步；LLM Provider 档案照常进 vault 同步 | agent 配置文件本身是本机的，同步指针会让他机显示从未写入过的指向；照 SSH「档案同步、materialize 本机」先例 | settled |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| LLM Provider | 一份 AI 服务接入档案：base url、凭据引用、可用模型集；可被切换指向 | 新造；与 `internal/provider`（git/server 同步后端）无关；已入 `CONTEXT.md` |
| Coding Agent | 接入 LLM 的编程助手 CLI/IDE；senv 可改写其配置以指向某 provider+model | 新造；复用 `senv mcp install` 已建立的 agent registry 概念；已入 `CONTEXT.md` |
| 模型目录 | models.dev `api.json` 提供的 provider/model 公开数据，落盘缓存供 add 时填充模型集 | 新造；已入 `CONTEXT.md` |
| 切换 | 把 Coding Agent 指向指定 LLM Provider 及模型的动作；senv 唯一事实源，写 agent 配置生效 | 新造；已入 `CONTEXT.md`；_Avoid_ 激活（env 分组含义） |
| 当前指向 | 单个 Coding Agent 当前使用的 LLM Provider 与模型记录；本机状态，不随 vault 同步 | 新造；本机语义对齐「解锁缓存」；已入 `CONTEXT.md` |

## ADR 候选

<!-- 仅记录满足「难逆转 + 后人费解 + 真实取舍」三门槛的；由 design.md 吸收或随 change 归档晋升 -->

- [ ] adr-plaintext-key-after-switch: LLM Provider 凭据存 vault，但切换后明文落在 agent 配置中——安全工具写明文 key 是接受的妥协，因为 agent 自身无法从 vault 拉取（出处：D4）
- [ ] adr-local-agent-pointer: Coding Agent 的当前指向是本机状态、不进 vault，因为 agent 配置本身是本机的，同步指针会与本机实况脱节（出处：D11）

## 未决问题

无
