---
name: senv-cli
description: 使用 senv client 的 CLI/MCP 安全读写环境变量、文本块、配置文件，管理分组、SSH 资产与 LLM Provider，并为 agent 配置 senv 接入。当任务涉及本机 senv 数据、senv CLI/MCP 工具或 senv 命令开发时使用。
metadata:
  version: "1.5"
---

# senv：agent 使用指南

senv 是本仓库的 CLI：AES-256-GCM 加密存储环境变量（env）、文本块（text）、配置文件（config），按 group 组织，支持 `{{env:group:key}}` / `{{text:group:key}}` 交叉引用。数据目录可用 git 同步（git provider），也可接入 senv-server（server provider，见下文）。

## 维护约定（给开发本仓库的 agent）

- 本文档是 agent 使用 senv 的权威说明。**新增/修改/删除 `cmd/` 下用户可见的命令、flag、交互/安全约束，或 MCP 工具清单与语义变化时，必须同步更新本文档**，并与功能变更一并提交。
- 事实来源：`senv --help`、`senv <cmd> --help`、`senv mcp list-tools`。不要凭记忆写命令用法。本机已安装的二进制可能落后于仓库 HEAD，开发本仓库时以 `go run . <cmd> --help` 为准。
- 子命令 help 无法覆盖的行为（交互提示、明文落盘、外部配置改写、凭据是否进入 argv/config）以实现、测试和 ADR 为准；有变化时也要回写本指南。

## agent 的两条访问路径

1. **MCP（优先）**：若宿主 agent 已配置 senv MCP server，直接调用 `senv` 前缀工具。当前共 21 个：env/text/group/config 各一组，另有只读 `ssh_host_list/get`、`llm_provider_list`、`llm_agent_status`、`mcp_server_list`。完整清单与描述以 `senv mcp list-tools` 为准。MCP server 无法提示密码；读写前确认用户已启动 session。
2. **CLI 兜底/管理面**：直接执行 `senv ...`。CLI 覆盖 MCP 不暴露的敏感管理操作，例如 keypair 导入/materialize、LLM Provider 写入与 coding agent 切换。

给 agent 安装 MCP 接入：`senv mcp install <claude-code|claude-desktop|cursor|codex|zcode|kimi|pi>`。先 `--print` 检查配置；只有用户明确要求时再落盘。`--all` 安装全部，`--scope project` 部分支持项目级配置。安装后需重启宿主 agent 生效。

`senv mcp install` 写的是 **senv 自己**的 MCP server；用户自己的 MCP server 定义用 `senv mcp add/export` 管理，见下文「MCP Server 档案与导出」。两件事不要混用。

## 非交互执行规则（重要）

- senv 解密需要密码，提示走 TTY。**管道/脚本环境里任何可能触发密码提示的命令都会卡住**——执行前先 `senv session status` 确认有活跃会话（它同时给出状态、原因与下一步）；没有会话就停下来让用户 `senv session start`。会话只是临近到期而非失效时，可用 `senv session refresh` 免密延长（见「会话（session）」）；agent 不要尝试替用户输密码。
- 需要 env 注入 shell 时用 `eval "$(senv env export --if-session)"`：无会话时静默退出 0，不会卡。
- 这些命令是交互式的，agent 不要用：`senv tui`、`senv interactive`、`senv config edit`、不带值/不带 `--file` 的 `senv text set`（TTY 下会开编辑器）、根快捷 `senv <group:key>` 不带值（同样可能开编辑器）。
- headless/CI 无安全内存存储时需 `senv session start --insecure-cache`（密钥落盘 0600），仅在用户明确要求时使用。默认 `session.auto_start=false`：临时认证用完即弃，不会因为一次密码输入就落盘会话。
- 需要凭据的 LLM Provider 命令默认走 TTY prompt；非交互场景使用管道 stdin 或 `--key-ref`，不要把凭据放进 argv、日志或回复。
- `senv mcp install`、`senv mcp export/unexport`、`senv ai switch`、`senv keypair materialize`、删除/覆盖/force push 都会写本机或外部状态。除只读查询外，先确认用户明确要求；不确定时先用 dry-run、`--print`、list/get 验证。

## 会话（session）

- **按 vault 分槽**：每个 data path（Abs+Clean+已存在前缀解析符号链接后）对应一个缓存槽，槽名是路径 hash 的 16 位十六进制。切 vault 不会覆盖另一个 vault 的会话；旧版本的单槽缓存按 hash 匹配收养，不匹配则保留并提示一次 `senv session clear --all`。
- **状态四态**：`Active` / `Expired`（duration 到期）/ `Invalidated`（重启后 `restart`、槽不匹配）/ `Unverifiable`（boot ID 读不到、缓存损坏、同槽多份）。`senv session status` 输出状态、原因、剩余时间与下一步。
- **续期**：`duration` 会话在业务命令复用 key 时滑动延长，但不超过绝对上限 `session.max_lifetime`（默认 24h）；`restart` 会话不看时间、只按 boot ID 判定，因此 `duration` 会话可跨重启存活到到期。只读命令（`session status`、`doctor`）不续期。
- **`senv session refresh`**：只用缓存 key 延长，**从不提示密码**；过期/失效/不可判定时报原因与下一步，不新建会话、不删缓存。agent 需要延长会话时优先用它。
- **`senv session start`**：已有有效会话时免密续期并保留原 timeout 策略；否则提示一次密码写入新会话。
- **`senv session clear`**：默认只清当前 vault；`--all` 清所有槽位加旧单槽残留。
- **不可判定不销毁**：`Unverifiable` 的缓存可能是唯一能解密数据的钥匙，默认保留不清理；排查原因后重试，确要丢弃才用 `senv session clear --all`。
- **MCP 绑定到 vault**：每个请求按 `keyHash+saltHash+dataPathHash` 校验，session ID 只进审计。**重新 `session start` 不再撤销正在运行的 MCP**；`session clear`、salt 变化（rekey/换密码）才会拒绝，需要时重启 MCP server。

## TUI 键位（人机交互，agent 不驱动）

`senv tui` 面向人操作，agent 不要驱动它；用户问「TUI 里怎么改 X」时按下面回答（细节以界内 `?` 键位总览为准）。

- 全局：`Tab`/`Shift+Tab` 循环；`1`–`9` 按注册顺序直达（越界忽略）；`S` 跨类型搜索（只匹配标识：key/name、host alias/hostname、provider alias，不匹配值/私钥/凭据）；`?` 键位总览；`q` 退出（仍有待推送时先提示一次）。
- 编辑面：Env `e` 内联编辑、`n` 新建、`d` 删除、`r` 重命名（分组栏改分组/条目栏改 key）、`a`/`x` 激活停用分组、`+` 新建分组、`y` 复制；Text/Config `e` 走 vim、`n`/`d`、`r` 重命名；Text 另有 `i` 从文件导入、`o` 导出；Config 另有 `m` 编辑分组与描述、`x` 导出、`i`/`u`（`I`/`U` 批量）安装卸载（需在计划页确认）。
- 多字段编辑走统一表单：`tab`/`↑↓` 切字段、`enter` 提交、`esc` 取消（无副作用），校验失败内联报错且保留输入；重命名是存储层原子操作，内容/权限不变。Env/Text 的 `default` 分组不可改名或删除。
- SSH Tab：两栏（host / keypair）。host 栏 `n` 新建、`e` 表单编辑、`d` 删除、`x` 导出选中 host 的 OpenSSH 片段；keypair 栏 `n`/`i` 导入、`R` 重命名（自动联动 host `identityKey`）、`d` 删除、`m` materialize、`x` 导出全部。host 表单里 proxyJump/identityKey 用选择器关联，引用不存在会在表单内联报错且不写入；`extra` 走 `$EDITOR`。被引用 keypair 默认拒绝删除并列出引用者，按 `F` 才强制删除并清空 host `identityKey`。
- AI Tab：两栏（provider / agent）。provider 栏 `n` 新建、`e` 编辑（别名只读）、`d` 删除、`enter` 详情；agent 栏 `↑↓` 选择、`s` 以左栏选中 provider 切换（`space` 多选 Agent 模型集，进入默认全选 → 选定默认模型 → 确认；空集不可提交）、`m` 对已指向的 agent 仅换默认模型，候选限定在该 agent 已写入的模型集内（未指向时提示先按 `s`）。agent 行展示 `provider / 默认模型（N 个模型）`，指针模型已不在档案中时附 `⚠` 漂移标记。provider 表单覆盖 base_url、`api_shape`、目录来源、模型集、默认模型与凭据来源；凭据默认从既有 env/text 条目中选择，也可选「新建自有凭据」用遮蔽输入写入 `text:llm-keys/<alias>`，明文不进 TUI 状态或渲染文本。枚举/引用字段聚焦时下方列出候选值。
- 只读详情：Config/SSH/AI 列表按 `enter` 打开详情弹层（长 `base_url`、模型列表、路径在列表里截断显示）。
- 同步状态：server 模式且未关闭 `auto_sync` 时底部常驻「N 条待推送 / 已同步 时间」；启动不等待网络——本地数据先行渲染，远端拉取在后台完成（2 秒预算，`--refresh` 绕过节流窗口），应用了变更会提示「已从 server 更新 N 条」并自动更新各标签；写操作后后台异步推送（2 秒预算）；git 模式不显示也不拉取。
- TUI 写操作会进本机操作审计（`senv audit` 可见），target 只含 group/key/name 等标识，不含值。

## 关键行为

- **寻址**：多数命令接受 `group:key` 地址（如 `prod:API_KEY`），地址中的 group 优先于 `-g/--group`。
- **快捷写入的两种语义**：根命令 `senv <group:key> [value]` 是 **text 写入**（如 `senv notes:TODO "内容"`）；env 写入的快捷形式是 `senv env <group:key> <value>`（如 `senv env prod:API_KEY "sk-xxx"`）。不带值的根快捷形式会走 stdin/编辑器，agent 避免使用。
- **引用解析**：存储值可含 `{{env:g:k}}` / `{{text:g:k}}`。`get` 默认原样输出，加 `-d/--decode` 解析；解析失败报错，加 `--loose` 保留未解析引用。`env export` 与 MCP `senv_env_export` 自动解析。
- **text set 输入优先级**：`--file` > stdin 管道 > 参数 > 编辑器。agent 写入文本块用 `--file` 或管道，避免触发编辑器。
- **最小暴露**：`env list` 会输出 `key=value`（值超过 50 字符截断），MCP `senv_env_list` 返回完整 key→值映射；`text list` 只显示 key、大小、更新时间。不要把 list 输出或密钥值复述进日志、回复。
- **默认分组**：未指定时用 `default`；`env export` 只导出已 activate 的 env 分组（`senv env group activate <name>`）。

## SSH 资产

- `keypair` 只导入既有 private key，不生成新密钥：`senv keypair import <name> --file <private-key>`；`list` 只看指纹/元数据。
- `senv keypair rename <old> <new>` 在同一次 mutation 内原子改写引用它的 host `identityKey`；目标名已存在时拒绝且不写入。
- `keypair materialize <name>` 会把 private key 明文写到 `~/.ssh/senv/<name>`（目录 0700、文件 0600）。仅在用户明确要求时使用；删除 vault 记录不会自动删除已落盘文件。
- `host` 管理结构化连接档案，可引用 keypair：`senv host add web --hostname ... --user ... --port ... --keypair web-key`；`--attr`/host `extra` 按 OpenSSH 原样直传，不要接受不可信值。
- `senv host export [--host web] [--output <file>]` 渲染 OpenSSH config 片段，不输出 private key。写文件前先向用户确认目标路径。

## LLM Provider 与 coding agent

- 公共目录操作不需要解锁 vault：`senv ai refresh`、`senv ai catalog status`、`senv ai status`。
- 档案与凭据存 vault：`senv ai provider add/edit/list/show/remove`。`show`/`list` 不返回凭据明文；`add` 禁止 `--api-key`，用 TTY prompt、`--api-key-stdin` 或 `--key-ref env:<group>/<key>`。HTTP base URL 必须显式 `--allow-http`。
- `senv ai provider edit <alias>` 就地编辑：alias 是主键不可改；只改传入的字段，省略的保持原值。轮换自有凭据用 `--rotate-key`（TTY）或 `--api-key-stdin`；改走外部引用用 `--key-ref`（会删除原自有凭据）。任一步失败不留部分更新。
- 接入地址统一按 OpenAI 兼容形态落库：`add`/`edit` 会补末段 `/v1` 并收敛尾斜杠，改写时提示；已归一的输入静默通过。`list`/`show` 展示的是落库值。
- `--api-shape`（`openai-chat` | `openai-responses` | `anthropic`）可选声明接口形态；`--api-shape ""` 清除回推断。留空时 `switch` 按目标 agent 协议族归一接入地址；声明后成为兼容判据，形态与目标 agent 协议族不匹配时 `switch` 拒绝写文件并提示「改档案形态或换 provider」。
- `senv ai switch <claude-code|codex|kimi|pi|opencode> <provider> [--models m1,m2] [--default-model D]` 会事务式改写目标 coding agent 的原生配置并保存本机指向。**省略 `--models` 即全选 Provider 模型集**，显式给出时保序（逗号分隔或重复给出均可，去重保序）；`--default-model` 只覆盖本次写入的起始模型，**不回写档案**。`--model` 已移除：出现即以非 0 退出并提示替代用法（参数校验发生在解锁与写盘之前）。接入地址按 agent 协议族写回：claude-code（Anthropic Messages）剥离末段 `/v1`，其余保持带版本形态；命令输出 provider、默认模型、模型集条数与实际写入的接入地址。切换后多数 agent 配置中会出现解密后的 API key（文件 0600）；Codex 只写环境变量名。
- Agent 模型集按各 agent 原生机制落盘，使 agent 自己的模型选择器能在集合内换模型：claude-code 写 `modelPicker`（`replaceBuiltInOptions: true`，每行带 `behavesAs` 映射到已知 Claude 模型，避免新版本把自定义模型当未知模型告警）、codex 生成 `~/.codex/model-catalogs/senv-<alias>.json` 并让 `model_catalog_json` 指向它、kimi 每个模型一条 `[models."senv-<alias>/<m>"]`、pi 写 `providers.<id>.models[]`（若 `settings.json` 已有非空 `enabledModels`，同时把本次默认模型置顶并加入 `senv-<alias>/*`，否则 PI 会优先选 scope 首个模型而不是默认模型）、opencode 写 `provider.<id>.models{}`。模型集超过 20 个时命令提示可用 `--models` 缩小。
- 切换会清理上一次 senv 写入、本次不再需要的条目，并删除不再被任何 agent 指向的 `senv-<alias>.json` catalog；**用户自有条目与自有文件既不改也不删**。缩集或换 provider 后重跑一次 `senv ai switch` 即可对齐。
- `senv ai status` 免解锁可用，显示 `provider / 默认模型（N 个模型）` 与切换时间；vault 已解锁且指针里的模型已不在档案中（档案缩集/改名）时附漂移提示，判定只看指针与档案、**不解析 agent 配置文件**，档案不可得时省略提示。TUI AI Tab 的 `m` 是「仅换默认模型」入口（限定在已写入的 Agent 模型集内）。
- MCP 只提供 provider 档案与 agent 指向的只读查询；不能通过 MCP 添加 provider 或切换 agent。

## MCP Server 档案与导出

- 档案存 vault，别名唯一标识；V1 只支持 `stdio`：`senv mcp add github --command npx --arg -y --arg @modelcontextprotocol/server-github --env GITHUB_TOKEN={{env:secrets:GH_TOKEN}}`。`--arg` 可重复且保序；`--env KEY=VALUE` 可重复，值里的 `{{env:...}}`/`{{text:...}}` 按模板原样存储、导出时才解析。
- `senv mcp list` 只列别名/传输/命令/env 键名，**不输出值**；`senv mcp get <alias>` 才展示完整字段（含值），是 CLI 解密面。`senv mcp edit <alias>` 就地改字段（别名不可改；传 `--arg` 替换整个参数列表，传 `--env` 替换整个 env 集合，`--unset-env KEY` 删单个键）。`senv mcp delete <alias>` 只删档案，不动任何 agent 配置。
- 导出：`senv mcp export --agent codex,cursor` 或 `--all`（必须显式给目标，没有默认全量）。按目标 agent 的格式合并写入其**全局配置**：JSON 族写 `mcpServers`，Codex 写 `[mcp_servers.<alias>]`。`--dry-run` 只出计划，`--print` 只输出片段，二者都不落盘。
- **明文落盘**：导出会把解析后的 env 值明文写进 agent 配置文件（0600，覆盖前备份 `<file>.bak`）。计划里会标出哪些条目含明文，执行前需确认；agent 与用户确认是必要前提，不要把值复述进回复或日志。
- 漂移与覆盖：senv 用本机台账 `~/.config/senv/mcp-exports.json`（不进 vault、不同步）判断条目是否由自己写入；目标条目被本地改过或是别人写的，默认拒绝覆盖，需 `--force`。台账损坏时按「全部外部条目」处理。
- 撤回：`senv mcp unexport --agent <id>|--all [alias...]`，依据台账移除；与 senv 写入内容一致的直接删除，被本地改过的需逐条确认。删除档案不会自动撤回已导出的条目。
- `command` 原样写入，不做绝对路径归一（`npx`/`uvx` 依赖 agent 自身 PATH）；不透传 `disabled`/`autoApprove` 等 agent 特有键。
- MCP 工具只提供 `mcp_server_list`（alias/传输类型/描述，不含值与 env 键名）；导出与写入只能在 CLI 做。

## server 模式

git provider 之外，vault 可托管在 senv-server 上：

- 接入：管理员签发一次性注册码 → `senv server register --address <url> --code <code>`；或全新机器直接 `senv init --server <url>`（token 默认取 `SENV_SERVER_TOKEN`）。vault 密码永不上传。
- 同步：`senv sync`（server provider 为增量 pull + 按条目乐观锁 push）。冲突时默认不改任何一侧，用 `--accept-remote`（以远端为准）或 `--force-push`（以本地为准）解决；`--no-interactive` 禁用交互式解决器。
- 历史与恢复：`senv history [kind:group:key]`（如 `senv history env:prod:API_KEY`）查看 server 保留的密文历史，`--restore <revision>` 恢复（会产生新 revision）。仅 server 模式支持；git 模式用 `git log`。
- 迁移：`senv migrate to-server` / `from-server` 在本地 git vault 与 server vault 间迁移。

## 常用命令速查

```bash
senv session status                      # 有活跃会话才能非交互读写；状态/原因/下一步
senv session refresh [-t 8h]             # 免密延长当前 vault 会话（从不弹密码）
senv session clear [--all]               # 默认只清当前 vault；--all 清所有槽位
senv env get prod:API_KEY                # 取值（默认 raw；加 -d 解析引用）
senv env set prod:API_KEY "sk-xxx"       # 写入（等价 -g prod API_KEY ...）
senv env list [prod]                     # 输出 key=value（截断），勿复述进日志
senv text set --file notes.md docs:README
senv text get -d docs:README
senv config list && senv config export <name> [--path <target>]
senv config create <name> --source <file> --target <path>
senv config install [--all|--group g] --dry-run   # 确认计划后加 --yes 执行
senv env group list | add | activate | deactivate
senv git sync                            # commit + pull --rebase + push 数据目录
senv push -m "msg"                       # 等价 git add+commit+push；--only 只 push
senv sync                                # 按 settings 走 git 或 server provider
senv doctor                              # metadata 与数据文件密钥一致性诊断（git pull 后可跑）
senv audit --since 2026-09-01            # 本机审计日志（只记元数据不记值）
senv keypair list                        # 只看 SSH keypair 元数据/指纹
senv host list                           # 只看 SSH host 连接元数据
senv ai status                           # 查看 coding agent 的 provider/默认模型（N 个模型）；已解锁时附漂移提示
senv ai provider edit <alias> [flags]    # 就地编辑档案（别名不可改；--api-shape 声明/清除形态）
senv mcp add github --command npx --arg -y --arg @modelcontextprotocol/server-github
senv mcp list && senv mcp get github     # list 不含值；get 展示完整字段
senv mcp export --all --dry-run          # 先看计划与明文落盘点
senv mcp export --agent codex,cursor     # 确认后写入 agent 全局配置
senv mcp unexport --agent codex          # 撤回（被本地改过的需逐条确认）
```

完整清单以 `senv --help` 与 `senv mcp list-tools` 为准；本文档滞后时以命令输出为准并回写修正。
