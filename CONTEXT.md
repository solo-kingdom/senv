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
