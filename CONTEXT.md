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

**安全存储（Secure Store）**:
持久会话的落盘位置：macOS 登录钥匙串，或经校验的内存文件系统；只有显式选择磁盘逃生舱才落持久磁盘。
_Avoid_: 缓存文件（泛指时）、Keychain（泛指平台存储时）

**工作副本（Working Copy）**:
client 本地目录中的加密数据文件，是唯一的编辑现场；同步通道只做 push/pull。

### 日志与历史

**操作审计（Operation Audit）**:
client 侧记录的本机业务操作流水（做了什么、对哪个条目、何时、结果），不含值；仅本机存储、本机查看。
_Avoid_: 审计日志（泛指 server 日志时）、操作日志

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
随 LLM Provider 档案保存的 per-model 信息，至少包含 context window；切换写入 Coding Agent 配置时优先使用它，再进行 agent 专属投影。增改模型集时缺失 context window 会拒绝写入；既有旧档案可不补全并继续读取。
_Avoid_: 模型配置（易与 agent 配置混淆）、模型能力（范围过宽）

**Coding Agent**:
接入 LLM 的编程助手 CLI/IDE，以 id 标识（如 claude-code、codex）；senv 通过改写其配置把它指向某个 LLM Provider，并写入 Agent 模型集与默认模型。
_Avoid_: agent（泛指时）、客户端

**Agent 模型集**:
某次切换后写入某个 Coding Agent、由该 agent 在自己的模型选择器里切换的模型集合；取自 Provider 模型集，默认全选，随切换时的选择而定。
_Avoid_: 模型列表（泛指时）、可用模型（指 provider 侧时）

**默认模型**:
切换后 Coding Agent 起始使用的那个模型；默认沿用 provider 档案的默认模型，可被单次切换覆盖且不回写档案。
_Avoid_: 首选模型、主模型

**切换**:
把某个 Coding Agent 指向指定 LLM Provider，并为其选定 Agent 模型集与默认模型的动作；senv 是唯一事实源，改写 agent 配置后即时生效。
_Avoid_: 激活（指 env 分组时）

**当前指向**:
单个 Coding Agent 最近一次被切换后的 LLM Provider、Agent 模型集与默认模型记录；属于本机状态，不随 vault 同步。
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

**MCP 安装（Install）**:
把 senv 自身的 MCP server（`senv mcp serve`）写入某个 Coding Agent 配置的动作，与「导出」区分：安装写的是 senv 这个 server，导出写的是用户的 MCP Server 档案。
_Avoid_: 用它指导出用户档案

**全局配置（Global Config）**:
Coding Agent 的 user 级配置文件（如 `~/.claude.json`、`~/.codex/config.toml`），作用域是当前用户的所有项目；区别于 project 级配置。
_Avoid_: 用户配置（易与 server 侧 user 混淆）
