---
name: senv-cli
description: 使用 senv client 的 CLI/MCP 安全读写环境变量、文本块、备份块、配置文件，管理分组、SSH 资产与 LLM Provider，并为 agent 配置 senv 接入。当任务涉及本机 senv 数据、senv CLI/MCP 工具或 senv 命令开发时使用。
metadata:
  version: "1.22"
---

# senv：agent 使用指南

senv 是本仓库的 CLI：AES-256-GCM 加密存储环境变量（env）、文本块（text）、备份块（backup）、配置文件（config），按 group 组织，支持 `{{env:group:key}}` / `{{text:group:key}}` 交叉引用（`backup` 不是合法引用 type）。数据目录可用 git 同步（git provider），也可接入 senv-server（server provider，见下文）。

## 维护约定（给开发本仓库的 agent）

- 本文档是 agent 使用 senv 的权威说明。**新增/修改/删除 `cmd/` 下用户可见的命令、flag、交互/安全约束，或 MCP 工具清单与语义变化时，必须同步更新本文档**，并与功能变更一并提交。
- 事实来源：`senv --help`、`senv <cmd> --help`、`senv mcp list-tools`。不要凭记忆写命令用法。本机已安装的二进制可能落后于仓库 HEAD，开发本仓库时以 `go run . <cmd> --help` 为准。
- 子命令 help 无法覆盖的行为（交互提示、明文落盘、外部配置改写、凭据是否进入 argv/config）以实现、测试和 ADR 为准；有变化时也要回写本指南。

## agent 的两条访问路径

1. **MCP（优先）**：若宿主 agent 已配置 senv MCP server，直接调用 `senv` 前缀工具。当前共 25 个：env/text/backup/group/config 各一组，另有只读 `ssh_host_list/get`、`llm_provider_list`、`llm_agent_status`、`mcp_server_list`。完整清单与描述以 `senv mcp list-tools` 为准。MCP server 无法提示密码；读写前确认用户已启动 session。每次工具调用都会写本机审计事件 `op_mcp_tool`（target 为工具名，不含值，`senv audit` 可见）；text 工具对保留组 `llm-keys` 一律拒绝读写（含 `{{text:llm-keys/...}}` 引用解析）——LLM API key 只能经 CLI/TUI 管理。`senv_backup_list` 不含正文；`senv_backup_get` 无 decode。`senv_group_add`/`senv_group_list` 接受 `kind=backup` / `group=backup`。
2. **CLI 兜底/管理面**：直接执行 `senv ...`。CLI 覆盖 MCP 不暴露的敏感管理操作，例如 keypair 导入/export/设默认、LLM Provider 写入与 coding agent 切换。

给 agent 安装 MCP 接入：`senv mcp install <claude-code|claude-desktop|cursor|codex|zcode|kimi|pi>`。先 `--print` 检查配置；只有用户明确要求时再落盘。`--all` 安装全部，`--scope project` 部分支持项目级配置。安装后需重启宿主 agent 生效。依赖外部扩展才能读取配置的目标（pi）会在输出里列出前置依赖，并在写盘前自动尝试安装；安装失败/找不到安装器只提示，不阻断写盘。

`senv mcp install` 写的是 **senv 自己**的 MCP server；用户自己的 MCP server 定义用 `senv mcp add/export` 管理，见下文「MCP Server 档案与导出」。两件事不要混用。

## 非交互执行规则（重要）

- senv 解密需要密码，提示走 TTY。**管道/脚本环境里任何可能触发密码提示的命令都会卡住**——执行前先 `senv session status` 确认有活跃会话（它同时给出状态、原因与下一步）；没有会话就停下来让用户 `senv session start`。会话只是临近到期而非失效时，可用 `senv session refresh` 免密延长（见「会话（session）」）；agent 不要尝试替用户输密码。
- 需要 env 注入 shell 时用 `eval "$(senv env export --if-session)"`：无会话时静默退出 0，不会卡。
- 这些命令是交互式的，agent 不要用：`senv tui`、`senv interactive`、`senv config edit`、不带值/不带 `--file` 的 `senv text set` / `senv backup set`（TTY 下会开编辑器）、根快捷 `senv <group:key>` 不带值（同样可能开编辑器）。
- Linux 无安全内存存储时：交互式（TTY）`senv session start` 在检测失败后先弹 y/N 确认，同意即写磁盘逃生舱（密钥明文 0600，附一次警告）完成初始化，拒绝才报错；无 TTY（管道/CI）仍直接报错，须显式 `senv session start --insecure-cache`（密钥落盘 0600）。落盘仅在用户明确要求/确认时使用。stock Darwin 无 tmpfs 时 `session start` 默认写入同一磁盘逃生舱，写入时警告一次，后续命令静默，不必每次加 flag。默认 `session.auto_start=false`：临时认证用完即弃，不会因为一次密码输入就落盘会话。旧版钥匙串会话不会被读取，需重新 `session start`。
- 需要凭据的 LLM Provider 命令默认走 TTY prompt；非交互场景使用管道 stdin 或 `--key-ref`，不要把凭据放进 argv、日志或回复。
- `senv mcp install`、`senv mcp export/unexport`、`senv ai switch`、`senv host export`（应用模式）、`senv host unexport`、`senv keypair export`/`set-default`/`clear-default`、`senv keypair prune`、删除/覆盖/force push 都会写本机或外部状态。除只读查询外，先确认用户明确要求；不确定时先用 dry-run、`--print`、list/get 验证。

## 会话（session）

- **按 vault 分槽**：每个 data path（Abs+Clean+已存在前缀解析符号链接后）对应一个缓存槽，槽名是路径 hash 的 16 位十六进制。切 vault 不会覆盖另一个 vault 的会话；旧版本的单槽缓存按 hash 匹配收养，不匹配则保留并提示一次 `senv session clear --all`。
- **状态四态**：`Active` / `Expired`（duration 到期）/ `Invalidated`（重启后 `restart`、槽不匹配）/ `Unverifiable`（boot ID 读不到、缓存损坏）。`senv session status` 输出状态、原因、剩余时间（Active 还给出 `Session cap` 绝对上限剩余）与下一步。
- **续期**：`duration` 会话在业务命令复用 key 时滑动延长，但不超过绝对上限 `session.max_lifetime`（默认 24h）；`restart` 会话不看时间、只按 boot ID 判定，因此 `duration` 会话可跨重启存活到到期。只读命令（`session status`、`doctor`）不续期。
- **`senv session refresh`**：只用缓存 key 延长，**从不提示密码**；过期/失效/不可判定时报原因与下一步，不新建会话、不删缓存。agent 需要延长会话时优先用它。
- **`senv session start`**：已有有效会话时免密续期并保留原 timeout 策略；否则提示一次密码写入新会话。写缓存时安全存储不可用（如 Linux 容器 /tmp 非 tmpfs）：TTY 下先问 y/N 是否改落磁盘逃生舱，拒绝才报错；非 TTY 直接报错（CI 需 `--insecure-cache`）。
- **`senv session clear`**：默认只清当前 vault；`--all` 清所有槽位加旧单槽残留。
- **只有到期才自动清缓存**：`Expired` 才清；`Invalidated` 与 `Unverifiable` 一律保留，因为缓存可能是另一个 vault 的唯一恢复钥匙。需要丢弃时显式 `senv session clear` / `--all`。
- **多缓存新者优先**：同一槽位同时有平台存储与逃生舱两份缓存时，按 `created_at` 选新的一份完成校验并复用，保留另一份；时间戳完全相同才报错要求 `senv session clear --all`。被选中的缓存仍须过 salt/key 校验。
- **重认证根因**：需要重新输密码时，错误给出根因（`expired`/`restarted`/`vault-changed`/`multiple-cache`/`unreadable`/`metadata-replaced`）**加一条确定动作**。`multiple-cache` 与 `unreadable` 不会引导去输密码（输密码也修不好），agent 遇到时应报告根因而不是尝试 `session start`。
- **MCP 绑定到 vault**：每个请求按 `keyHash+saltHash+dataPathHash` 校验，session ID 只进审计。**重新 `session start` 不再撤销正在运行的 MCP**；`session clear`、salt 变化（rekey/换密码）才会拒绝，需要时重启 MCP server。

## 耗时日志（perf log）

- 关键路径耗时（启动各阶段、vault 各域装载、同步请求、本地扫描）超过阈值时追加 JSON 行到 `~/.log/senv/perf.log`；默认开启、阈值 100ms。与操作审计（`audit.log`）分离：perf 记"花了多久"，审计记"做了什么"。
- `SENV_PERF=off` 整体关闭；`SENV_PERF_THRESHOLD=<毫秒>` 调阈值（非法或 ≤0 回退默认）。用户反馈「senv 慢」时先看这份日志定位阶段，不需要额外 debug 开关。

## TUI 键位（人机交互，agent 不驱动）

`senv tui` 面向人操作，agent 不要驱动它；用户问「TUI 里怎么改 X」时按下面回答（细节以界内 `?` 键位总览为准）。TUI 界面语言为英文；底栏展开当前场景的操作快捷键（如 `e edit · n new · d delete · / filter · ?`），随焦点栏/表单/确认/向导/过滤切换；导航键与次要动词不进底栏，完整键位按分组列在 `?` 总览里。Search/`?` overlay 打开时底栏改用 overlay 自身键位。

- 全局：`Tab`/`Shift+Tab` 循环；`1`–`9` 按注册顺序直达（越界忽略）；`Ctrl+R` 刷新当前 Tab；`S` 跨类型搜索（只匹配标识：key/name、host alias/hostname/group/tags、provider alias、MCP 档案 alias/command、backup 的 group/key/description，不匹配值/私钥/凭据/MCP env 值/backup 正文）；`?` 键位总览（与实际键位同源，不会漂移）；`esc` 回上一层（清过滤/关弹层/向导回退）；`q` 退出（仍有待推送时先提示一次）。列表 Tab 通用导航：`↑↓/jk`、`←→/hl` 切栏、`g`/`G` 跳顶底、`PgUp/PgDn` 翻页。
- Env/Text/Backup/Config 均为分组侧栏双栏：侧栏顶部 All 伪组（默认选中，聚合全部条目，行前缀 `group/key`），其下各组带条目计数（随 `/` 过滤更新；Text/Backup 空分组计数 0 也显示）；`←→/hl` 切栏。组操作：`t` 激活/停用（env，default 不可停用）、`r` 重命名、`d` 删除、`+` 新建（env/text/backup，**必填说明**）；All 上无组操作（提示选择具体分组）。条目操作：Env `e` 内联编辑、`n` 新建、`d` 删除、`r` 重命名、`y` 复制、`v` 显隐、`D` 解引用；Text `e` vim、`n`/`d`、`r` 重命名、`i` 导入、`x` 导出；Backup 同 Text 但无 `D`；Config `e` vim、`n` 创建、`r` 重命名、`m` 元信息、`x` 导出、`i`/`u`（`I`/`U` 整组/全部）安装卸载（计划页仅 `esc`/`n` 取消）。确认弹窗统一 `enter`/`y` 确认、`esc`/`n` 取消。
- 多选：条目栏 `space` 勾选/取消、`a` 全选当前过滤可见集（再按取消）；选择跨过滤持久，面板标题提示「已选 N（M 被过滤）」。批量安全动词（Env/Text/SSH `d`、Text/SSH `x`、Config `i`/`u`、MCP `x`/`u`/`X`/`U`）作用于多选集，走既有确认/计划流；选择集为空回落游标单条；`e`/`r`/`m`/详情需单选；提交后清空选择集。
- 多字段编辑走统一表单：`tab`/`↑↓` 切字段、`enter` 提交、`esc` 取消（无副作用），校验失败内联报错且保留输入；Env `n` 新建与 Config `n` 创建也走结构化表单（Config 收集名称/源路径/target/分组/描述；Env 的 value 为遮蔽输入，说明可选），创建失败可在表单内修正。重命名是存储层原子操作，内容/权限不变。Env/Text 的 `default` 分组不可改名或删除。Host/KeyPair/AI Provider 表单同样有可选说明。
- SSH Tab：两栏（分组侧栏 / host）。侧栏：All 伪组置顶 → 组名字母序 → 「未分组」置底（仅在有未归类 host 时出现），选中组决定 host 栏集合，`←→/hl` 两栏切焦点（切入 host 栏定位该组第一条）。host 栏 `n` 新建、`e` 表单编辑（含 group 自由文本与 tags 字段）、`d` 删除、`x` 导出选中 host 的 OpenSSH 片段（焦点在侧栏时导出全部；预览超高时可 ↑↓/jk/PgUp/PgDn/`g`/`G` 滚动，`w` 再填目标文件写入；批量/单条导出表单拒绝 `~/.ssh/senv` 内部路径——该树由应用导出自持、外来文件会被幽灵清理，提示改用 `A`）、`A` 应用导出（与 `senv host export` 同一 `Manager.Apply` 编排：host 栏重建游标 host 所在组的整组片段，侧栏重建选中组、All = 全量重建并清理幽灵组片段；确认框列出组片段数/待落盘私钥数/Include 注册状态/warning 计数，`enter`/`y` 执行、`esc`/`n` 取消，结果以摘要 toast 呈现），`u` 撤回导出（两栏均可用；异步预检后若无可撤回项 toast 直达，否则确认框列出将移除的 Include 注册行、将删除的组片段数，并明示 `~/.ssh/senv/keys/` 落盘私钥保留；`enter`/`y` 执行与 `senv host unexport` 同一编排，`esc`/`n` 取消零副作用），行尾内联 tags（`#tag` 最多 2 个、超出 `+n`），行内 `key:name(fp)` 内联引用 keypair 名称与指纹摘要；`/` 过滤匹配 alias/hostname/tags/group；刷新统一 `Ctrl+R`。host 表单里 proxyJump/identityKey 用选择器关联，引用不存在会在表单内联报错且不写入；`extra` 走 `$EDITOR`。
- KeyPair Tab：独立 Tab（`mgr.SSH` 非空时紧随 SSH Tab），两栏（分组侧栏 / keypair 列表），组语义与 host 一致（All 置顶 → 字母序 → 「未分组」置底）。列表栏 `n`/`i` 导入（表单含 name/private key file/group）、`r` 重命名（自动联动 host `identityKey`）、`e` 编辑 group（仅 group 字段的小表单）、`d` 删除、`A` 落盘（与 `senv keypair export` 同一路径，含伴生 `.pub`）、`D` 设/取消本机默认（写 `groups/_default.conf` 的 `Host *`）、`p` prune（两栏均可用；异步列出未被任何 host 引用的落盘私钥与 `.pub`，当前默认钥跳过；vault 中仍有对应 keypair 的标注 `(keypair still in vault)`；`enter`/`y` 删除与 `senv keypair prune` 同一白名单，`esc`/`n` 取消零删除；vault 档案永不触碰），`enter` 详情（公钥无缩进、展示 OpenSSH comment/邮箱与是否默认），`v` 按需预览私钥（详情弹层，关闭即丢弃；列表永不渲染私钥），`/` 按名称过滤；行内 `被 N 个 Host 引用`、`default` 标记、零引用灰显「未被引用」。被引用 keypair 默认拒绝删除并列出引用者，按 `F` 才强制删除并清空 host `identityKey`；export 确认后只提示落盘路径（含 `.pub`）。
- AI Tab：两栏（provider / agent）。provider 栏 `n` 新建、`e` 编辑（别名只读）、`r` 重命名（联动自有凭据与本机 agent 指针；不改写 coding agent 原生配置，需重跑 `senv ai switch`）、`d` 删除、`enter` 详情、`/` 过滤（匹配 alias）、`R` 联网刷新公开模型目录缓存（与 `senv ai refresh` 同语义，失败保留旧缓存；`Ctrl+R` 仍是本地档案重载、不访问网络）；agent 栏 `↑↓` 选择、`s` 以左栏选中 provider 切换（`space` 多选 Agent 模型集，进入默认全选 → 选定默认模型 → 确认；空集不可提交）、`M` 对已指向的 agent 仅换默认模型（与 `s` 成对，大写为变体），候选限定在该 agent 已写入的模型集内（未指向时提示先按 `s`）。agent 行展示 `provider / 默认模型（N 个模型）`，指针模型已不在档案中时附 `⚠` 漂移标记。provider 表单覆盖 base_url、`api_shape`、目录来源、模型集、模型上下文、模型输出、模型推理、默认推理档、输入模态、默认模型与凭据来源；模型上下文用 `<model>=<tokens>` 逗号分隔，默认推理档用 `<model>=<effort>` 或集合级单一档位，输入模态用 `<model>=<mod>[,<mod>...]`。凭据默认从既有 env/text 条目中选择，也可选「新建自有凭据」用遮蔽输入写入 `text:llm-keys/<alias>`，明文不进 TUI 状态或渲染文本。枚举/引用字段聚焦时下方列出候选值。详情弹层展示各模型已保存的 context / output / reasoning / 默认推理档 / 输入模态。
- MCP Tab：两栏（档案 / 导出目标 agent，agent 集合与 `senv mcp export` 相同，含 claude-desktop/cursor）。档案栏 `n` 新建、`e` 编辑（别名只读）、`d` 删除（不自动撤回）、`i` 导入（路径表单，`~` 展开；与 `senv mcp import` 同一解析/传输识别/冲突跳过语义，完成后弹出结果报告：逐条 create/conflict/failed 及原因 + 计数，不含值；TUI 不提供 `--dry-run`）、`enter` 详情、`/` 过滤（匹配 alias/command）；`s` 在 user/project 间切换导出 scope（会话内有效、不持久化，默认 user；目前仅 cursor 区分，project 写 CWD 相对的 `.cursor/mcp.json`，其余 agent 两 scope 同路径）；`x`/`u` 当前档案 × 当前 agent，`X`/`U` 当前档案 × 全部 agent（均按当前 scope）。表单含 transport 选择（stdio/http/sse）：stdio 下填 command/args/env，http/sse 下只出现 url/headers（headers 经 `$EDITOR` 按 `Name: Value` 行编辑）。计划页 `y` 确认 / `esc` 取消 / `F` 覆盖漂移；撤回被改过的条目逐条 `y/n`。列表/计划是摘要，不渲染值（remote 列表只显示 `scheme://host` 与 env 键数）；详情弹层完整渲染字段：url 含 query、header 为 `Name: Value`、env 为 `KEY=value`（模板引用原样）。表单预览只显示键名，值仅在 `$EDITOR` 内可见。`mcp install` / `serve` / `list-tools`、`--print` 仍走 CLI。
- 只读详情：Config/SSH/KeyPair/AI/MCP 列表按 `enter` 打开详情弹层（长 `base_url`、模型列表、路径在列表里截断显示）。
- 同步状态：server 模式且未关闭 `auto_sync` 时底部常驻「N 条待推送 / 已同步 时间」；启动不等待网络——本地数据先行渲染，远端拉取在后台完成（2 秒预算，`--refresh` 绕过节流窗口），应用了变更会提示「已从 server 更新 N 条」并自动更新各标签；写操作后后台异步推送（2 秒预算）；git 模式不显示也不拉取。
- 启动性能：只加载当前聚焦 Tab，其余 Tab 首次聚焦时才加载（已加载不重复）；存在加密本地快照（`<dataPath>/tui-snapshot.enc`，vault 主密钥 AES-256-GCM，0600）时首屏先渲染快照、后台真实解密比对后替换，快照缺失/损坏/指纹不符时静默回退直接解密；`SENV_TUI_SNAPSHOT=off` 关闭整个快照缓存。慢启动定位用 `SENV_PERF_THRESHOLD=1 senv tui` 后看 `~/.log/senv/perf.log`。
- TUI 写操作会进本机操作审计（`senv audit` 可见），target 只含 group/key/name 等标识，不含值。

## 关键行为

- **寻址**：多数命令接受 `group:key` 地址（如 `prod:API_KEY`），地址中的 group 优先于 `-g/--group`。
- **快捷写入的两种语义**：根命令 `senv <group:key> [value]` 是 **text 写入**（如 `senv notes:TODO "内容"`），MUST NOT 写入 backup；env 写入的快捷形式是 `senv env <group:key> <value>`（如 `senv env prod:API_KEY "sk-xxx"`）；backup 用 `senv backup <group:key> [value]` 或 `senv backup set`。不带值的根快捷形式会走 stdin/编辑器，agent 避免使用。
- **引用解析**：存储值可含 `{{env:g:k}}` / `{{text:g:k}}`。`get` 默认原样输出，加 `-d/--decode` 解析；解析失败报错，加 `--loose` 保留未解析引用。`{{backup:…}}` 不是合法 type，保持字面量、不会去取 backup 正文。`env export` 与 MCP `senv_env_export` 自动解析；**引用目标缺失时保留 `{{...}}` 模板、stderr 打 warning（含 env key 名）、命令仍 exit 0**；循环引用与超深度仍失败。
- **text set 输入优先级**：`--file` > stdin 管道 > 参数 > 编辑器。agent 写入文本块用 `--file` 或管道，避免触发编辑器。
- **text import/export**：`senv text import <key|group:key> --file <path>` 把文件内容加密入库（upsert：已存在 key 直接覆盖并刷新 `updated_at`，无确认提示；源文件保持不动；`--file` 必填，缺失即报错，不回落 stdin/编辑器）。`senv text export <key|group:key> --path <path>` 把明文值原子落盘，固定 0600（覆盖既有宽松文件会收紧；目标或父目录为符号链接时拒绝；内容逐字节原样导出、**不经引用解析**——要解码导出用 `text get -d -o`）；成功只打印路径、不回显明文；导出是读取面，不新增审计事件。export 写出明文文件，执行前先向用户确认。
- **backup**：独立 kind（`backups/{group}/{key}.enc`），不与 text 混用。上限 `MaxBackupSize`（512KB，只计 value 明文，超限拒绝不截断）。`senv backup set/get/list/delete/import/export` 与 `backup group list/add/delete`；`backup get` **无** `-d/--decode/--loose`；`backup list` 只显示 key、大小、时间与说明，不含正文。set 输入优先级与 text 相同（`--file` > stdin > 参数 > 编辑器）；import/export 语义与 text 对齐（`--file`/`--path` 必填、upsert 无确认、0600、拒符号链接、成功只打印路径）。backup 无 activate/deactivate，也不参与引用。
- **最小暴露**：`env list` 会输出 `key=value`（值超过 50 字符截断，有说明时另起一行展示），MCP `senv_env_list` 返回 `group → key → {value, description}`；`text list` / `backup list` 只显示 key、大小、更新时间（有说明则附上）。不要把 list 输出或密钥值复述进日志、回复。
- **说明（vault note）**：env/text/backup 条目、env/text/backup 分组、config、Host、KeyPair、LLM Provider 档案、MCP Server 档案都可以带一段短说明（trim 后最多 2048 字节 UTF-8，不准当密钥用）。条目/档案的 `--description` 可省略（空说明合法）；**更新时未传 flag 则保留原说明**。MCP `senv_env_set` / `senv_text_set` / `senv_backup_set` 的 `description` 同理（省略 = 保留）。
- **默认分组**：未指定时用 `default`；`env export` 只导出已 activate 的 env 分组（`senv env group activate <name>`）。init 会建 env `default`、text `default`/`llm-keys`，以及 backup `default`（存量 vault 打开 backup Manager 时幂等补建）。
- **禁止隐式建组**：`env set` / `text set` / `text import` / `backup set` / `backup import` 在组不存在时失败，不再自动建组。新建 env/text/backup 组必须显式：`senv env group add <name> --description "..."`、`senv text group add <name> --description "..."`、`senv backup group add <name> --description "..."`、MCP `senv_group_add`（`kind` 为 env/text/backup，`description` 必填非空）。Host/KeyPair 组仍是档案上的自由文本，不走这套登记。
- **同名覆盖 warning**：多个已激活 env 组出现同名 key 时，`senv env export` 与 `senv env group activate` 在 stderr 打 warning（列出组名与 export 采用哪一组），命令仍 exit 0。MCP `senv_env_export` / `senv_group_activate` 成功时带 `warnings`。`default` 被后激活组覆盖是合法用法。
- **分组命名（约定，软件不强制）**：env 组有两种用法，**先判断 key 再选组**。写入前 `group list`。组名禁止用 `/` 冒充层级。env 组与 text 组同名也不表示同一组。

  **变体组**（外部工具按名读的契约 key，同名不同值，靠 activate 切换；不要为每个服务新开一组）：

  | 组 | 场景 |
  |----|------|
  | `default` | 个人/兜底 |
  | 裸项目名 | 工作/项目：`feg`、`pt`、`vela`、`zilta` |

  **存档组**（自用 key，加服务或机器前缀消撞名，默认不激活，只为查找与 `{{env:...}}` 引用）：

  | 组 | 用在 | key 形态 |
  |----|------|----------|
  | `svc` | 自建服务统装，**不要**再开 `svc-<服务>` | `LITELLM_UI_PASSWORD` |
  | `infra` | 共享基础设施账号 | 已有前缀习惯可保留 |
  | `ai` / `keys` / `notes` / `llm-keys` | 存量桶，复用、不开同义组 | — |

  `device-*` 只留「激活哪台机器就用哪套身份」的项；同一服务多台机器、脚本要同时看见两套密码时，用 key 前缀（`LNV_` / `MSABJ_`）放进存档组，不要再按机器拆组。SSH Host/KeyPair 仍按组织/项目（`feg` / `mv` / `ym` / `default`），不要每台主机一组。说明字段、禁止隐式建组、同名覆盖 warning 已落地（ADR-0026）。
- **env/text/backup 写入带说明**：`senv env set <group:key> <value> --description "..."`、`senv text set <group:key> --file … --description "..."`、`senv text import … --description "..."`、`senv backup set <group:key> --file … --description "..."`、`senv backup import … --description "..."`。省略 `--description` 不改已有说明。

## SSH 资产

- `keypair` 只导入既有 private key，不生成新密钥：`senv keypair import <name> --file <private-key>`；`--group` 设单值归属分组（空 = 未分组，TUI KeyPair Tab 按组侧栏浏览）；`list` 只看指纹/元数据，有值时行尾展示 `group:<name>`。
- `senv keypair rename <old> <new>` 在同一次 mutation 内原子改写引用它的 host `identityKey`；目标名已存在时拒绝且不写入。
- `senv keypair edit <name> --group <g>` 免编辑器改分组或说明（`--group` 与 `--description` 至少给一个；key 材料不动；空 `--group` = 未分组；组名禁止含 `/`）。改组只影响未来 export 路径，已落盘文件不迁移（用 `keypair prune` 清理旧路径残留）；若该钥是本机默认，会改写 `_default.conf` 的 IdentityFile。两个 flag 都未给时报参数错误，不进编辑器。说明不写入密钥文件或 OpenSSH comment。
- `senv keypair export <name>`（`materialize` 为过渡别名）把 private key 明文写到 `~/.ssh/senv/keys/<分组>/<名>`（未分组入 `_ungrouped`；目录 0700、文件 0600），并在能派生公钥时写伴生 `<名>.pub`（0644）；与 `host export` 应用模式的落盘路径相同。仅在用户明确要求时使用；删除 vault 记录不会自动删除已落盘文件（用 `keypair prune` 清理未引用文件）。
- `senv keypair set-default <name>` 把该钥设为本机 OpenSSH `Host *` 兜底：必要时先落盘，再写 `~/.ssh/senv/groups/_default.conf` 并注册 Include。**只存本机、不进 vault、不同步**。`senv keypair clear-default` 删该片段。`list` 在当前默认钥行尾标 `default`。rename/改组/删除默认钥会改写或清除该片段。`host unexport` 删光 `groups/` 时一并清掉默认。
- `host` 管理结构化连接档案，可引用 keypair：`senv host add web --hostname ... --user ... --port ... --keypair web-key`；`--group` 设单值归属分组（空 = 未分组，TUI 按组侧栏浏览；组名禁止含 `/`）、`--tag` 加多值标注（可重复）、`--description` 写 vault 说明（不进 ssh config）；`senv host edit <alias> --group <g>` 免编辑器改分组或 `--description`；`host list` 行尾展示 `group:<name>`（有值时），`host get` 展示 Group/Tags/说明。MCP `ssh_host_list`/`ssh_host_get` 只读返回说明。`--attr`/host `extra` 按 OpenSSH 原样直传，不要接受不可信值。
- `senv host export` 默认是**应用模式**：按分组整树维护 `~/.ssh/senv/`（`groups/<组>.conf` 组片段 + `keys/<组>/<名>` 落盘私钥与伴生 `.pub`），自动落盘缺失的被引用 keypair（私钥已存在跳过不覆盖，缺失的 `.pub` 仍补写），并幂等注册 `~/.ssh/config` 顶部一行 `Include ~/.ssh/senv/groups/*.conf`——导出后 ssh 直接可用，无需手工接线。Host Apply **不改写** `groups/_default.conf`；全量幽灵清理也跳过它。`--group <名>` 只重建该组片段；`--host <别名>` 重建其所在组整文件；组在 vault 消失时仅全量导出清理对应组片段。`--output <file>|-` 是纯渲染模式（stdout/文件片段，零副作用）。`senv host unexport` 摘除注册行并删除组片段（含 `_default.conf`；不碰 vault 与落盘私钥）；`senv keypair prune [--force]` 列出并清理未被任何 host 引用的落盘私钥（非交互终端必须 `--force`；当前默认钥的落盘文件视为引用，跳过）。写文件类操作执行前先向用户确认。片段中 `IdentityFile` 指向的 keypair 不在本机 vault 时（如 host 档案先同步到、keypair 还没到），stderr 逐条 `warning: host <alias> 引用的 keypair <name> 不在本机 vault（可能尚未同步）`，片段照常生成（按未分组占位路径渲染），keypair 同步到后的下一次导出自动收敛。真实 Host 组名撞 `_ungrouped` / `_default` 时导出报错。

## LLM Provider 与 coding agent

- 公共目录操作不需要解锁 vault：`senv ai refresh`、`senv ai catalog status`、`senv ai status`。
- 档案与凭据存 vault：`senv ai provider add/edit/rename/list/show/remove`。`show`/`list` 不返回凭据明文；`add`/`edit` 可带 `--description`（档案说明，不是模型目录文案）；`add` 禁止 `--api-key`，用 TTY prompt、`--api-key-stdin` 或 `--key-ref env:<group>/<key>`。HTTP base URL 必须显式 `--allow-http`。MCP `llm_provider_list` 只读返回说明。
- 新增或替换模型集时必须校验每个模型的 context window：`--catalog-provider` 从 models.dev `limit.context` 读取；自定义模型或目录缺字段时用重复的 `--model-context <model>=<tokens>` 显式提供。缺失即报错且不写档案；已有档案不强制迁移，仍可读取，需要时可 `senv ai provider edit <alias> --model-context ...` 补齐。输出上限、推理档位、默认推理档与输入模态可显式设置：重复的 `--model-output <model>=<tokens>`、`--model-reasoning <model>=<effort>[;<effort>...]`（档位分号分隔；非空即视为推理模型）、`--model-default-reasoning <model>=<effort>`、`--model-modalities <model>=<mod>[,<mod>...]`，以及集合级 `--default-reasoning <effort>`（只填充有档位且尚未解析出默认档的模型）。有档位的模型必须声明默认推理档，且该值必须属于档位列表；senv 不从列表首项或模型名推断。TUI 表单对应「模型输出」「模型推理」「默认推理档」「输入模态」字段。`show`/MCP 的 `model_info` 展示已保存的 context window/output/reasoning/default_reasoning/input_modalities。
- `senv ai provider edit <alias>` 就地编辑：alias 是主键不可改；只改传入的字段，省略的保持原值。元数据 flag 传入但无有效值（如 `--model-output ""`）表示清空该维度；TUI 编辑表单清空字段同义。轮换自有凭据用 `--rotate-key`（TTY）或 `--api-key-stdin`；改走外部引用用 `--key-ref`（会删除原自有凭据）。任一步失败不留部分更新。
- `senv ai provider rename <old> <new>`：同次操作内改档案别名；规范自有凭据 `text:llm-keys/<old>` 一并改名并更新 `credential_ref`；**env 全部分组中值精确等于 `{{text:llm-keys/<old>}}` 的条目改写为 `{{text:llm-keys/<new>}}`**（如 codex switch 写入的 SeedRef）；本机 agent 指针中的 provider 字段同步改写。目标别名或目标 `llm-keys/<new>` 已存在时拒绝且零写入。外部 `--key-ref` 不移动。不改写 coding agent 原生配置或 catalog——输出提醒重跑 `senv ai switch <agent> <new>`。TUI AI Tab `r` 等价。
- 接入地址统一按 OpenAI 兼容形态落库：`add`/`edit` 会补末段 `/v1` 并收敛尾斜杠，改写时提示；已归一的输入静默通过。`list`/`show` 展示的是落库值。
- `--api-shape`（`openai-chat` | `openai-responses` | `anthropic`）可选声明接口形态；`--api-shape ""` 清除回推断。留空时 `switch` 按目标 agent 协议族归一接入地址；声明后成为兼容判据，形态与目标 agent 协议族不匹配时 `switch` 拒绝写文件并提示「改档案形态或换 provider」。
- `senv ai switch <claude-code|codex|kimi|pi|opencode> <provider> [--models m1,m2] [--default-model D]` 会事务式改写目标 coding agent 的原生配置并保存本机指向。**省略 `--models` 即全选 Provider 模型集**，显式给出时保序（逗号分隔或重复给出均可，去重保序）；`--default-model` 只覆盖本次写入的起始模型，**不回写档案**。`--model` 已移除：出现即以非 0 退出并提示替代用法（参数校验发生在解锁与写盘之前）。接入地址按 agent 协议族写回：claude-code（Anthropic Messages）剥离末段 `/v1`，其余保持带版本形态；命令输出 provider、默认模型、模型集条数与实际写入的接入地址。切换后多数 agent 配置中会出现解密后的 API key（文件 0600）；**Codex 只写 `env_key` 名，明文不落盘**，且该名字保证可由 `senv env export` 提供——`credential_ref` 为 `env:<g>/<k>` 时 `env_key` 就是 `<k>`（shell 里既有的名字，零额外操作）；为 `text:<g>/<k>`（含自有凭据 `text:llm-keys/<alias>`）时是 `SENV_<ALIAS>_API_KEY`（alias 净化大写、`-` 变 `_`），切换会在**默认 env 组**补一条同名引用条目（值 `{{text:<g>/<k>}}`，已存在同名条目则不覆盖）。因此 codex 取凭据的前提是 `eval "$(senv env export --if-session)"` 已生效：引用组未激活时切换会给出可操作 warning（`senv env group activate <g>`），同名条目被占用且值不同时也会提示核对；已运行的 `codex app-server`（Codex Desktop / SSH 远程）只在启动时拷贝环境，export 后需 `pkill -f 'codex app-server'` 再重开，否则 TUI 仍报 `Missing environment variable`（同机 `codex exec` 却能通）。档案跨机同步后 `credential_ref` 指向的条目可能尚未在本机：`switch` 保持 fail-closed（零写入，codex 同样生效），错误指明缺失的完整引用（如 `text:llm-keys/<alias>`）与修复指引（`senv text add` 或从已有机器同步）。
- Agent 模型集按各 agent 原生机制落盘，使 agent 自己的模型选择器能在集合内换模型：claude-code 写 `modelPicker`（`replaceBuiltInOptions: true`，每行带 `behavesAs` 映射到已知 Claude 模型，避免新版本把自定义模型当未知模型告警）、codex 生成 `~/.codex/model-catalogs/senv-<alias>.json` 并让 `model_catalog_json` 指向它、kimi 每个模型一条 `[models."senv-<alias>/<m>"]`、pi 写 `providers.<id>.models[]`（若 `settings.json` 已有非空 `enabledModels`，同时把本次默认模型置顶并加入 `senv-<alias>/*`，否则 PI 会优先选 scope 首个模型而不是默认模型）、opencode 写 `provider.<id>.models{}`。模型集超过 20 个时命令提示可用 `--models` 缩小。
- 模型元数据随切换投影进各 agent，避免落到内置默认值（pi 缺省 context 128k/output 16k、opencode 缺省 limit 0、kimi 无 thinking）：context window 写 pi `contextWindow`、opencode `limit.context`、kimi `max_context_size`、codex catalog `context_window`；输出上限写 pi `maxTokens`、opencode `limit.output`、kimi `max_output_size`；推理能力写 pi/opencode `reasoning: true`、kimi `capabilities=["thinking"]`+`support_efforts`、codex `supported_reasoning_levels` 与声明的 `default_reasoning_level`；输入模态写 Codex `input_modalities`、kimi `image_in`/`video_in`、pi `input`、opencode `modalities.input`，按各 agent schema 取值范围收敛：pi 仅透传 text/image、codex 仅 text/image/audio（models.dev 的 audio/pdf/video 等超出值在投影时丢弃，全被过滤时 pi 省略 `input`、codex 回退 `["text"]`），opencode/kimi 原样保留。未知字段一律省略而不是写 0/false；codex catalog 除外——无档位或旧档案缺默认推理档时必须写单档 `none`（并设 `default_reasoning_level`），缺输入模态写 `["text"]`，空数组会让 Codex `/model` 选择器回车无法退出。不从档位列表首项推断默认档。旧档案缺声明仍可切换、不回写 vault，输出会提示可 `edit` 补全。
- 档案显式声明的 `api_shape` 还决定 OpenAI 兼容族内的线协议：pi `api` 字段与 kimi provider `type` 在 `openai-completions`/`openai`（chat）与 `openai-responses`/`openai_responses`（responses）间选择，codex `wire_api` 在声明 `openai-chat` 时写 `chat`，opencode 在声明 `openai-responses` 时用 `@ai-sdk/openai`；未声明时维持各自既有默认。
- 切换会清理上一次 senv 写入、本次不再需要的条目，并删除不再被任何 agent 指向的 `senv-<alias>.json` catalog；**用户自有条目与自有文件既不改也不删**。缩集或换 provider 后重跑一次 `senv ai switch` 即可对齐。
- `senv ai status` 免解锁可用，显示 `provider / 默认模型（N 个模型）` 与切换时间；vault 已解锁且指针里的模型已不在档案中（档案缩集/改名）时附漂移提示，判定只看指针与档案、**不解析 agent 配置文件**，档案不可得时省略提示。TUI AI Tab 的 `M` 是「仅换默认模型」入口（限定在已写入的 Agent 模型集内）。
- MCP 只提供 provider 档案与 agent 指向的只读查询；不能通过 MCP 添加 provider 或切换 agent。

## MCP Server 档案与导出

- 档案存 vault，别名唯一标识。传输三选一，字段互斥：`stdio` 用 `--command`（必填）/`--arg`/`--env`；`http`/`sse` 用 `--url`（必填，http(s)）/`--header "Name: Value"`，不接受 command/args/env。`senv mcp add github --command npx --arg -y --arg @modelcontextprotocol/server-github --env GITHUB_TOKEN={{env:secrets:GH_TOKEN}}`；remote 示例：`senv mcp add web --transport http --url "https://api.example.com/mcp?key={{env:secrets:KEY}}" --header "Authorization: Bearer {{env:secrets:T}}"`。`--arg`/`--header` 可重复；url/header 值与 env 值一样按模板原样存储、导出时才解析。
- `senv mcp import <file> [--dry-run]`：把既有 agent 配置文件批量建档。JSON 读 `mcpServers` 对象（Claude/Cursor/ZCode/Kimi 惯例），`.toml` 读 `[mcp_servers.<alias>]`（Codex）。传输识别：显式 `type`（http/sse/stdio）优先，无 type 有 `url` 按 `http`，有 `command` 按 `stdio`；codex 的 `transport: streamable-http` 归一为 `http`。值原样保存；别名已存在报 conflict 跳过（**从不覆盖**）；个别条目失败不中止其余，命令以非零退出汇总。
- `senv mcp list` 只列别名/传输/命令（remote 显示 `scheme://host` 来源）/env 键名，**不输出值、url query 与 header**；`senv mcp get <alias>` 才展示完整字段（含值），是 CLI 解密面。`senv mcp edit <alias>` 就地改字段（别名不可改；`--transport` 可切换传输，切换后字段集整体替换并按目标传输校验，失败不落库；`--arg`/`--env`/`--header` 传了即整体替换，`--unset-env`/`--unset-header` 删单个键）。`senv mcp delete <alias>` 只删档案，不动任何 agent 配置。
- 导出：`senv mcp export --agent codex,cursor` 或 `--all`（必须显式给目标，没有默认全量）。按目标 agent 的格式合并写入其**全局配置**：JSON 族 stdio 写 `command/args/env`、remote 写 `type/url/headers`；Codex TOML 写 `url`（+`transport`）。`--dry-run` 只出计划，`--print` 只输出片段，二者都不落盘。
- **remote 能力矩阵**：目标 agent 配置格式表达不了的条目在计划里标 `error` 并说明原因，该 agent 文件不动、其余 agent 继续（已知：claude-desktop 不支持任何 remote 条目；codex remote 不支持 headers；zcode/kimi 未核验 sse）。senv 只写各 agent 文档化键，不猜键名：`type`/`transport` 只在声明的目标上写（pi、kimi 无该键，http 与 sse 落盘形状相同）。**pi 无内置 MCP**：配置写 `$PI_CODING_AGENT_DIR/mcp.json`（默认 `~/.pi/agent/mcp.json`），由 pi-mcp-adapter 扩展读取（`pi install npm:pi-mcp-adapter`，stdio/http/sse + headers 均支持）；此类适配器目标的前置依赖会随安装/导出计划输出，并在写盘前自动尝试安装（检测 `settings.json` 的 `packages` 已装则跳过；安装失败/不在 PATH 只提示，导出与写盘继续，失败不计入导出失败）。
- **明文落盘**：导出会把解析后的 env、url 与 header 值明文写进 agent 配置文件（0600，覆盖前备份 `<file>.bak`）。计划里会标出哪些条目含明文，执行前需确认；agent 与用户确认是必要前提，不要把值复述进回复或日志。
- **引用缺失宽松写入**：`{{env:...}}`/`{{text:...}}` 引用目标在本机缺失时不再终止该 agent 的写入——模板原文照写入文件、stderr 逐条 warning（含缺失的 env/text 名）、计划标注 `[未解析引用]`。典型场景是档案先随 sync 到新机器、凭据后补；补齐后重跑 export 即收敛（产生新指纹，按漂移语义处理）。
- 漂移与覆盖：senv 用本机台账 `~/.config/senv/mcp-exports.json`（不进 vault、不同步）判断条目是否由自己写入（指纹覆盖 url/headers）；目标条目被本地改过或是别人写的，默认拒绝覆盖，需 `--force`。台账损坏时按「全部外部条目」处理。旧版本导出的 stdio 条目指纹在升级后依然有效。
- 撤回：`senv mcp unexport --agent <id>|--all [alias...]`，依据台账移除（传输无关）；与 senv 写入内容一致的直接删除，被本地改过的需逐条确认。删除档案不会自动撤回已导出的条目。
- `command` 原样写入，不做绝对路径归一（`npx`/`uvx` 依赖 agent 自身 PATH）；不透传 `disabled`/`autoApprove` 等 agent 特有键。
- MCP 工具只提供 `mcp_server_list`（alias/传输类型/描述，不含值、env 键名、url 与 headers）；导出、导入与写入走 CLI 或 TUI MCP Tab，不能通过 MCP 工具做。

## server 模式

git provider 之外，vault 可托管在 senv-server 上：

- 接入（注册流程）：服务端 admin 签发一次性注册码 `senv-server admin create-registration <user> [--expires 30m] [--dsn ...]`（默认 30 分钟；明文码只打印一次，库中只存 SHA-256）→ 客户端 `senv server register --address <url> --code <code> --name <设备名> [--vault main]`。设备名 1–128 字符、不含控制字符；同用户同名 client 报冲突且**注册码不被消费**（换名重试即可）；无效/过期/已用码统一报「注册码无效或已过期」并计入来源 IP 限速（防枚举），注册成功返回一次性明文 token（同样只存哈希）。或全新机器直接 `senv init --server <url>`（token 默认取 `SENV_SERVER_TOKEN`）。vault 密码永不上传。
- token 存储：server token 存 `<configPath>/server-token.json`（0600，机器本地），**不在 settings.json**——settings.json 会被 git provider 同步，token 绝不能进 git 远端；`.gitignore`（覆盖 `server-token.json`、`mcp-exports.json`）自动生成，`git add` 亦有排除路径规格兜底。旧版 settings 内嵌 token 在下次读取时自动迁移。
- 同步：`senv sync`（server provider 为增量 pull + 按条目乐观锁 push）。同步通道覆盖 env/text/backup/config、LLM Provider 档案（`llm_providers/<alias>.enc`）、MCP Server 档案（`mcp_servers/<alias>.enc`）与 SSH 资产档案（`hosts/<alias>.enc`、`keypairs/<name>.enc`，KeyPair 档案含私钥本体）——新机器首次同步即拉到全部档案，凭 vault 口令即可取用，含 `senv backup get`；Coding Agent 切换指针、MCP 导出台账与本机默认密钥对是本机状态，不同步；`~/.ssh/senv/` 下已落盘文件与 `~/.ssh/config` 的 senv 注册行也是本机状态，不同步——新机器首次同步后跑一次 `senv host export`（应用模式）即自动重建组片段、落盘缺失私钥并完成注册（默认钥需在该机再 `keypair set-default`）。冲突时默认不改任何一侧，用 `--accept-remote`（以远端为准）或 `--force-push`（以本地为准）解决；`--no-interactive` 禁用交互式解决器。配置源档案（llm_provider/mcp_server/ssh_host/ssh_keypair）冲突时报告额外给出本地/远端 alias+revision 对照，提醒双端人工修改需核对；SSH 档案冲突只渲染元数据，不解码展示内容（防私钥泄露）；backup 冲突对齐 text（可解密对比，合并上限 512KB）。**含 `backup`/`backup_meta` 的 client 必须等 senv-server 镜像先升级**（白名单在 server 二进制里；发布顺序见 `docs/senv-server.md`，优先 iship 构建），否则整批 push 会被旧 server 拒绝。
- 历史与恢复：`senv history [kind:group:key]`（如 `senv history env:prod:API_KEY`）查看 server 保留的密文历史，`--restore <revision>` 恢复（会产生新 revision）。仅 server 模式支持；git 模式用 `git log`。
- 迁移：`senv migrate to-server` / `from-server` 在本地 git vault 与 server vault 间迁移。
- senv-server 管理面（独立二进制，不经 cobra、不在 `senv --help`；`--dsn` 缺省取环境变量 `SENV_SERVER_DSN`，任何能连到 PG 的主机都可执行）：
  - `admin create-user <name>`：建用户并签发一次性明文 token（只打印一次，库存 SHA-256）。
  - `admin create-registration <user> [--expires 30m]`：签发一次性注册码（默认 30 分钟）。
  - `admin revoke-token <token>`：按明文吊销单个 token（不可逆，不影响同用户其他 token）。
  - `admin list-clients [--user <name>]`：列出已注册设备（缺省全部用户）。
  - `admin block-client` / `unblock-client --client <名> [--user <user>]`：屏蔽/解封设备——屏蔽是其名下 token 立即失效的**可逆**状态，数据归属 user 不受影响；区别于按凭证的不可逆吊销。
  - `admin logs [--user u] [--client c] [--since d] [--until d] [--outcome OK|AUTH-FAILED|BLOCKED|RATE-LIMITED] [--limit n]`：查访问日志；`admin logs-prune --before <YYYY-MM-DD>` 清理旧日志。
  - `serve` / `migrate`：启动服务（启动前校验 schema 版本）与应用迁移；flags 与构建发布流程见 `docs/senv-server.md`。

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
senv text import docs:README --file ./README.md   # 文件加密入库（upsert，源文件不动）
senv text export secrets:KEY --path ./key.pem     # 0600 明文原子落盘（只打印路径）
senv backup set --file dump.txt notes:DUMP        # 独立 backup kind；无 -d
senv backup get notes:DUMP
senv backup import notes:DUMP --file ./dump.txt
senv backup export notes:DUMP --path ./dump.txt   # 0600，成功只打印路径
senv backup list notes                            # 不含正文
senv backup group add secrets --description "..."
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
senv ai provider rename <old> <new>      # 改名（联动自有凭据与指针；需重跑 switch）
senv mcp add github --command npx --arg -y --arg @modelcontextprotocol/server-github
senv mcp add web --transport http --url "https://api.example.com/mcp?key={{env:secrets:K}}" --header "Authorization: Bearer {{env:secrets:T}}"
senv mcp import ~/.claude.json --dry-run  # 批量导入既有配置（JSON mcpServers / Codex TOML）
senv mcp list && senv mcp get github     # list 不含值；get 展示完整字段
senv mcp export --all --dry-run          # 先看计划与明文落盘点（remote 不被支持的目标标 error）
senv mcp export --agent codex,cursor     # 确认后写入 agent 全局配置
senv mcp unexport --agent codex          # 撤回（被本地改过的需逐条确认）
```

完整清单以 `senv --help` 与 `senv mcp list-tools` 为准；本文档滞后时以命令输出为准并回写修正。
