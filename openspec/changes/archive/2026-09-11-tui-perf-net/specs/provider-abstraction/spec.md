## ADDED Requirements

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
