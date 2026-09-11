# provider-abstraction Specification

## Purpose
定义 senv 远端 provider 的选择与构造行为：本地加密文件存储始终是工作副本，provider 仅作为同步通道（git remote 或 senv-server），CLI 按配置统一构造。
## Requirements
### Requirement: 默认 provider 为 git

未做任何 provider 配置时，系统 SHALL 使用 git provider，行为与现状完全一致。MUST NOT 要求用户显式配置才能使用现有功能。

#### Scenario: 全新初始化

- **WHEN** 用户执行 `senv init` 且未配置 provider
- **THEN** 系统以 git 模式初始化，后续同步行为与现有 `git-sync` spec 一致

### Requirement: provider 配置显式可选

系统 SHALL 支持在配置中声明 provider 类型（`git` / `server`）及对应连接参数。配置了 server provider 但缺少必要参数（地址或凭据）时，MUST 给出明确错误并指出缺失项，MUST NOT 静默回退到 git。

#### Scenario: server 配置不完整

- **WHEN** 配置声明 provider 为 `server` 但缺少地址
- **THEN** 系统报错并指明缺失的配置项，不执行任何同步

### Requirement: 统一构造入口

CLI 各命令 SHALL 通过统一入口获取 provider 实例，MUST NOT 各自直接构造存储/同步组件。构造失败时错误信息 MUST 包含 provider 类型与原因。

#### Scenario: 构造失败信息可读

- **WHEN** provider 构造失败（如数据仓路径无效）
- **THEN** 错误信息包含 provider 类型与底层原因

### Requirement: git provider 行为保持

git provider 经抽象层暴露的同步行为 SHALL 与现有 `git-sync` capability 的约定逐条一致，包括提交、rebase 拉取、冲突中止与错误可读性。

#### Scenario: 抽象后回归

- **WHEN** 用户执行 `senv git sync`（经统一入口构造）
- **THEN** 行为与重构前完全一致，现有 git 测试套件全部通过

### Requirement: server provider 地址 scheme 校验

构造 server provider 时，系统 SHALL 默认仅接受 https 地址；非 https 地址 MUST 拒绝构造，错误信息 MUST 包含地址、拒绝原因与显式豁免方法。豁免（如内网 http）SHALL 通过显式环境变量开启，且开启时 MUST 在错误/日志路径保持可审计。git provider 不受此约束。

#### Scenario: http 地址默认被拒

- **WHEN** 用户将 provider 配置为 `http://example.com` 并执行任意需同步的命令
- **THEN** 构造失败，错误信息说明仅接受 https 及豁免方法，不发起任何请求

#### Scenario: 显式豁免后内网 http 可用

- **WHEN** 用户设置豁免环境变量后使用 `http://内网地址`
- **THEN** 构造成功，同步行为与 https 一致

#### Scenario: https 地址不受影响

- **WHEN** 用户使用 `https://` 的 server 地址
- **THEN** 校验通过，无需任何豁免设置

### Requirement: 进程内 provider 单例与连接复用

同一进程内，系统 SHALL 为同一份 provider 配置复用同一个 provider 实例及其底层 HTTP 连接：TUI 的后台 pull、写后 push 与 History 查询 SHALL 复用同一连接池；CLI 命令的 auto pull 与退出 AutoPush SHALL 复用同一实例。进程内的 TCP+TLS 新建连接数 SHALL ≤1（并发首请求导致的瞬时第二连接不视为违约，但 MUST 归入同一连接池复用）。provider 的选择语义（git 默认、settings 覆盖）、构造失败报错与 client 被屏蔽处理 MUST NOT 因单例化而改变。并发复用 MUST 数据竞争安全。

#### Scenario: TUI 启动只建一条连接

- **WHEN** server 模式下启动 TUI，后台 pull 与 History 查询先后发生
- **THEN** 进程内新建连接数为 1，第二次网络动作复用已有连接（以耗时日志 `conns_new` 维度可见）

#### Scenario: CLI 命令 pull 与 push 复用

- **WHEN** 执行一条会触发 auto pull 且退出时有待推送变更的 CLI 命令
- **THEN** auto pull 与 AutoPush 复用同一 provider 实例，进程内新建连接数为 1

#### Scenario: 配置变更后新进程生效

- **WHEN** 用户修改 settings 中 provider 配置后启动新进程
- **THEN** 新进程按新配置构造，不受单例化影响

