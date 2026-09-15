# senv

senv 是一个本地加密、多端同步的密钥/配置管理工具：client（CLI/TUI）持有口令并在本地解密工作，同步到 git remote 或 senv-server；server 只存密文（零知识）。

## Language

### 身份与信任

**User（用户）**:
server 侧的账户主体，拥有一个或多个 vault；由管理员创建。
_Avoid_: 账号、client

**Client（客户端设备）**:
注册到 server 的一台设备/一份安装，属于某个 user，有自己的名字与专属凭证，可被屏蔽。
_Avoid_: 设备 token、机器（泛指时）

**屏蔽（Block）**:
管理员对单个 client 设置的**可逆**禁止状态：该 client 的凭证即刻失效、请求被拒，但 client 记录与该 user 的 vault 数据均保留，解封后可恢复。
_Avoid_: 封禁（含不可逆意味时）、禁用

**吊销（Revoke）**:
单份凭证的**不可逆**作废，与 client 实体状态无关。沿用既有语义。
_Avoid_: 屏蔽（指凭证时）

**登录事件（Auth Event）**:
安全日志语境下，一次 API 请求的认证结果（成功或失败原因）。senv 无交互式登录会话。
_Avoid_: 登录/登出（会话含义）

### 本地状态

**配置源（Config Source）**:
人工添加进 vault、可随 vault 跨机分发的数据：env/text/config 条目、LLM Provider 与 MCP Server 档案、SSH 资产档案（Host 与 KeyPair，含私钥本体）——均为 SSH-style 加密 blob。与它相对的是**本机状态（同步边界语境）**——仅在单一机器上有意义、刻意不随 vault 同步的派生状态（当前指向、导出状态、落盘私钥、agent 配置文件）。"配置源同步、本机状态不同步"是同步通道的边界。
_Avoid_: 配置项（泛指 env 时）、同步数据（指协议载荷时）

**解锁缓存（Session Cache）**:
client 本地保存的口令派生密钥缓存，用于免重复输口令；与 server 无关。client 检测到被屏蔽时清除它，本地加密工作副本保留。
_Avoid_: 会话（server 会话含义）、session（歧义场合）

**持久会话（Persistent Session）**:
`senv session start` 落盘到安全存储的免密凭据，其有效期内所有命令可复用，直到到期、失效或被清除。
_Avoid_: 登录、登录状态、session（泛指时）

**临时认证（Ephemeral Auth）**:
无持久会话时，单次命令内由口令派生的密钥；仅当前进程内复用，不落盘、不改变会话状态。
_Avoid_: 会话、临时会话

**续期（Refresh）**:
持久会话仍有效时，凭缓存密钥重签有效期、不重新验证口令的动作；与需口令的 `session start` 区分。
_Avoid_: 刷新（泛指界面刷新时）、重新登录

**到期（Expired）**:
持久会话的有效时间点已过，与绑定前提是否变化无关。
_Avoid_: 过期（统称到期/失效/不可判定时）

**失效（Invalidated）**:
持久会话的成立前提不再满足（如系统已重启、所绑定的 vault 已变），与时间无关。
_Avoid_: 过期

**不可判定（Unverifiable）**:
环境故障导致无法确证持久会话有效或失效；缓存保留，既不按到期也不按失效处理。
_Avoid_: 过期、失效

**重认证（Re-authentication）**:
持久会话不可复用后，用户重新输入口令以恢复访问的动作；区别于首次临时认证。是本地体验指标的核心：每一次都应有确定的根因与下一步。
_Avoid_: 重新登录、登录、重新解锁

**重认证根因（Re-auth Root Cause）**:
触发重认证的可判定原因，取值与判定三层一致（到期 / 重启 / vault 不符 / 多缓存 / 环境不可判定 / metadata 失步）；`session status` 与命令错误必须给出同一根因。
_Avoid_: 过期原因（只覆盖到期时）、错误信息

**自动清理（Automatic Clearing）**:
系统在没有用户显式指令时删除持久会话缓存的动作；只允许发生在到期。失效与不可判定一律保留：缓存可能是另一个 vault 的唯一恢复钥匙。
_Avoid_: 清缓存（泛指时）、失效清理

**安全存储（Secure Store）**:
持久会话缓存的驻留位置：经操作系统确认的内存文件系统。操作系统钥匙串（含 macOS 登录钥匙串）不是安全存储，也不作为可选后端。
_Avoid_: 缓存文件（泛指时）、Keychain、钥匙串、密钥串

**磁盘逃生舱（Disk Escape Hatch）**:
安全存储不可用时，持久会话缓存落到用户磁盘：派生钥明文、仅本用户可读。它不是安全存储。由用户显式选择，或因平台无法提供经确认的内存文件系统而成为默认写目标。
_Avoid_: 不安全缓存（正式行文）、insecure-cache（指旗标时除外）

**工作副本（Working Copy）**:
client 本地目录中的加密数据文件，是唯一的编辑现场；同步通道只做 push/pull。

### 数据组织

**分组（Group）**:
条目（env 变量组、text 块、config 文件、Host）的单值归属：组是 TUI 分组侧栏的组织单位，也是导出激活的作用域。空分组值表示该条目未归入任何组，TUI 归入「未分组」兜底组。
_Avoid_: 目录、文件夹

**标签（Tags）**:
Host 的多值自由标注，与单值的 Group 正交：只用于列表行内展示与过滤，不参与分组侧栏。
_Avoid_: 分组（指 Group 时）

### 日志与历史

**操作审计（Operation Audit）**:
client 侧记录的本机业务操作流水（做了什么、对哪个条目、何时、结果），不含值；仅本机存储、本机查看。
_Avoid_: 审计日志（泛指 server 日志时）、操作日志

**耗时日志（Perf Log）**:
client 侧记录关键路径「花了多久」的本机日志：按阶段/操作记录耗时与规模维度，超阈值才记；仅本机存储查看，与操作审计分离。
_Avoid_: 审计日志（指操作审计时）、操作日志、性能日志（泛指 server 侧时）

**系统日志（Access Log）**:
server 侧记录的每个 API 请求与认证事件流水，存于数据库，仅服务器管理员可查，用于安全审计。
_Avoid_: 服务日志（stdout 含义）、审计（client 含义）

**配置历史版本（Entry History）**:
server 为单个条目保留的最近 N 个密文历史版本（默认 3），按 revision 回看，可恢复为当前值；仅 server 模式提供（git 模式用 git 历史）。
_Avoid_: 版本号（指 revision 本身时）、快照（指整库时）

### SSH 资产

**Host（主机）**:
SSH 连接档案：以别名为唯一标识，含真实地址、登录用户、端口等连接要素，可关联一把 KeyPair，其余连接参数以自由属性承载。
_Avoid_: 服务器（泛指远端机器时）、机器、节点

**别名**:
Host 的连接名与唯一标识，即 OpenSSH `Host` token；真实地址是 Host 的独立字段，不与别名混用。
_Avoid_: 展示名、备注名

**KeyPair（密钥对）**:
从既有私钥文件导入的 SSH 密钥资产：私钥是机密本体，公钥仅用于辨识（指纹/展示）；senv 不生成新密钥。
_Avoid_: identity 文件（指盘上路径时）、钥匙（单指私钥时）

**落盘（Materialize）**:
把 vault 内 KeyPair 的私钥按分组组织形式写到 `~/.ssh/senv/keys/` 下的动作及其产物（分组目录布局见 ADR-0023），并在能派生公钥时写出伴生 `.pub`；属本机状态，不同步。用户面 CLI/TUI 可称 `keypair export`（与 Host Apply 对称），领域仍称落盘；`materialize` 为过渡别名。
_Avoid_: 导出（指 Host 配置导出或本机默认密钥对写入时）、解密（泛指时）

**导出（Export）**:
Host 配置离开 vault、进入本机 OpenSSH 配置体系的动作总称：默认应用模式写组片段、自动落盘被引用密钥、注册 Include；`--output -` 的 stdout 预览是其纯渲染子模式。
_Avoid_: 落盘（指私钥写出时）、设为默认（指本机默认密钥对时）、同步（指 vault 同步时）

**组片段（Group Fragment）**:
一个分组的 Host 渲染产物：一组一文件落于 `~/.ssh/senv/groups/`（未分组入 `_ungrouped.conf`），senv 完全拥有、不回读合并的派生产物。同目录保留名 `_default.conf` 承载本机默认密钥对，不由 Host 档案渲染生成；Host 组名不得与 `_ungrouped` / `_default` 冲突（导出时报错）。
_Avoid_: 配置文件（泛指 `~/.ssh/config` 时）

**本机默认密钥对（Default KeyPair）**:
本机选定的一把 KeyPair，经设默认写入 `~/.ssh/senv/groups/_default.conf` 的 `Host *` + `IdentityFile`，作为未另行配置 Identity 的 SSH 连接的兜底身份；该文件为真源，仅本机、不同步，各机器可不同；`host unexport` 删组片段时一并清除。
_Avoid_: 默认密钥（含糊）、全局 identity、vault 默认（其不进 vault）

**撤回（Unexport）**:
导出的逆动作：移除 `~/.ssh/config` 中的 senv 注册行并删除组片段（含本机默认密钥对片段）；不删 vault 档案、不删落盘私钥。
_Avoid_: 卸载（指 senv 自身或 MCP 安装时）、删除（指删档案/私钥时）

**ProxyJump（跳板）**:
经另一台 Host 中转连接目标 Host；其值必须引用已存在的 Host 别名。
_Avoid_: 代理（泛指时）、前置机

**ProxyCommand**:
自定义连接命令文本，黑盒直传，不做结构化解析。
_Avoid_: 跳板（指 ProxyJump 时）

### LLM 接入

**LLM Provider**:
一份 AI 服务接入档案：以别名为唯一标识，含接入地址、凭据引用与可用模型集；凭据本体存于 vault。与同步后端 provider（git/server）无关。
_Avoid_: provider（单独使用，易与同步后端混淆）、服务商

**接入地址（Base URL）**:
LLM Provider 记录的统一接入点，按 OpenAI 兼容形态保存：路径以 `/v1` 结尾。它不是任何单个 Coding Agent 最终请求的地址。
_Avoid_: 端点（指具体 API 路径时）、服务地址

**接入形态（API Shape）**:
LLM Provider 可选声明的接口方言（`openai-chat` / `openai-responses` / `anthropic`）；缺省时不推断，写配置时按 Coding Agent 的协议族归一接入地址。
_Avoid_: 接口类型（泛）、协议（指 agent 侧协议族时）

**协议族**:
Coding Agent 使用的 API 方言，分 Anthropic Messages 与 OpenAI 兼容两族；同一份接入地址写进不同族 agent 的配置时形态不同。
_Avoid_: 兼容性处理（实现意味）、适配（泛指时）

**Provider 模型集**:
一份 LLM Provider 档案声明的全部可用模型；是所有切换的候选来源，与接入地址、凭据同属该档案。
_Avoid_: 模型列表（泛指时）、可用模型、模型目录（指公开数据源时）

**模型目录**:
models.dev 提供的 provider 与 model 公开数据；senv 缓存后用于填充 Provider 模型集。自定义模型不依赖它，但必须显式提供模型元数据。
_Avoid_: 模型列表（泛指时）

**模型元数据**:
随 LLM Provider 档案保存的 per-model 声明值，至少包含 context window；该模型若声明了推理档位，还必须有默认推理档。来源是显式提供或模型目录，senv 不按模型名或档位列表推断。切换写入 Coding Agent 配置时优先使用它，再进行 agent 专属投影。增改模型集时缺失必填项会拒绝写入；既有旧档案可不补全并继续读取。
_Avoid_: 模型配置（易与 agent 配置混淆）、模型能力（范围过宽）

**Coding Agent**:
接入 LLM 的编程助手 CLI/IDE，以 id 标识（如 claude-code、codex）；senv 通过改写其配置把它指向某个 LLM Provider，并写入 Agent 模型集与默认模型。
_Avoid_: agent（泛指时）、客户端

**Agent 模型集**:
某次切换后写入某个 Coding Agent、由该 agent 在自己的模型选择器里切换的模型集合；取自 Provider 模型集，默认全选，随切换时的选择而定。
_Avoid_: 模型列表（泛指时）、可用模型（指 provider 侧时）

**默认模型**:
切换后 Coding Agent 起始使用的那个模型；默认沿用 provider 档案的默认模型，可被单次切换覆盖且不回写档案。
_Avoid_: 首选模型、主模型、默认推理档

**默认推理档**:
某个模型未在会话里另选时使用的推理努力级别；是该模型的模型元数据声明值，必须属于其推理档位，不是默认模型，也不是从档位列表推导出的属性。仅当该模型声明了推理档位时才必填。
_Avoid_: 默认模型（指选哪个模型）、默认档（歧义）、启发式（推断意味）、reasoning level（行文用中文；落盘字段名可保留原文）

**输入模态**:
模型接受的输入类型集合（如 text、image）。有则写入模型元数据，缺席表示未知，不等于纯文本。
_Avoid_: 多模态（过宽）、视觉（只覆盖 image）、capabilities（实现旗标）

**切换**:
把某个 Coding Agent 指向指定 LLM Provider，并为其选定 Agent 模型集与默认模型的动作；senv 是唯一事实源，改写 agent 配置后即时生效。
_Avoid_: 激活（指 env 分组时）

**当前指向**:
单个 Coding Agent 最近一次被切换后的 LLM Provider、Agent 模型集与默认模型记录；属于本机状态（同步边界语境），不随 vault 同步——它是切换动作的本机结果，不是配置源。
_Avoid_: 指针（实现意味）、profile（多预设含义，未采用）

**漂移（Drift）**:
senv 侧事实源（Provider 模型集、MCP Server 档案）与 agent 侧派生产物（Agent 模型集、已导出条目）不再一致、且 senv 不回读 agent 配置去自动纠正的状态。
_Avoid_: 不一致（泛指时）、同步冲突（指 vault 同步时）、失配

### MCP 接入

**MCP Server 档案**:
一份第三方 MCP 服务器接入定义：以别名为唯一标识，含传输类型与启动/连接要素，存于 vault。senv 是它的唯一事实源，导出时按目标 Coding Agent 的配置格式落盘。
_Avoid_: MCP 配置（指 agent 配置文件里的落盘结果时）、服务（泛指时）

**导出（Export）**:
把 MCP Server 档案合并写入目标 Coding Agent 全局配置的动作；agent 配置文件是派生产物，不由 senv 回读为事实源。
_Avoid_: 安装（指 senv 自身的 MCP server 时）、同步（指 vault 同步时）

**撤回（Unexport）**:
按本机台账，从 Coding Agent 全局配置中移除由 senv 导出的条目；不删除 vault 中的 MCP Server 档案。
_Avoid_: 卸载（指 config uninstall 或 MCP 安装的反操作时）、删除（指删档案时）

**导出状态**:
某个 Coding Agent 上，一份 MCP Server 档案的本机导出结果：未导出 / 已导出 / 漂移。属于本机状态（同步边界语境），不随 vault 同步，对标 LLM 的当前指向；档案本体才是配置源。
_Avoid_: 安装状态、同步状态、MCP 配置（指文件内容时）

**MCP 安装（Install）**:
把 senv 自身的 MCP server（`senv mcp serve`）写入某个 Coding Agent 配置的动作，与「导出」区分：安装写的是 senv 这个 server，导出写的是用户的 MCP Server 档案。
_Avoid_: 用它指导出用户档案

**全局配置（Global Config）**:
Coding Agent 的 user 级配置文件（如 `~/.claude.json`、`~/.codex/config.toml`），作用域是当前用户的所有项目；区别于 project 级配置。
_Avoid_: 用户配置（易与 server 侧 user 混淆）

### TUI 呈现

**加载态（Loading）**:
Tab 面板发出数据请求后、结果返回前的过渡呈现。它必须与空态区分：空态里的操作指引（如「按 n 新建」）只允许在装载完成后出现。
_Avoid_: 用空态文案顶替加载态；「暂无 XX」（指装载未完成时）

**空态（Empty）**:
装载完成后数据集为空时的稳定呈现，携带下一步操作指引。
_Avoid_: 加载态（指装载未完成时）

**错误态（Error）**:
装载或操作失败时的呈现，展示失败原因；与空态区分——失败不是「没有数据」。
_Avoid_: 加载态、空态
