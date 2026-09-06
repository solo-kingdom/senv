## Purpose
client 在本机记录业务操作流水（何事、对哪个目标、何时、结果），不含任何值；提供 cli+tui 按日期查看，支撑事后追溯。

## ADDED Requirements

### Requirement: 业务操作事件记录
client 对 env/text/config 的增删改、config 安装/卸载、同步（push/pull）与冲突解决 SHALL 追加审计事件；事件 SHALL 包含操作类型、目标标识（kind/group/key 或文件名）、日期时间与结果（成功/失败），MUST NOT 包含任何值、明文内容或派生密钥材料。

#### Scenario: 记录 env 修改
- **WHEN** 用户成功修改 env 条目
- **THEN** 审计文件追加一条含操作类型、目标标识、时间戳与成功结果的记录

#### Scenario: 失败也留痕
- **WHEN** 某写操作失败
- **THEN** 记录含失败结果的审计事件，且不含导致失败的输入值

### Requirement: 审计不阻断业务
审计写入 SHALL 为 best-effort：写入失败时业务操作继续完成并仅给出告警；审计文件与目录权限 SHALL 维持 0600/0700。

#### Scenario: 审计文件不可写
- **WHEN** 审计文件被剥夺写权限后执行写操作
- **THEN** 业务操作正常完成，输出告警，命令退出码不受影响

### Requirement: cli 查看
`senv audit` SHALL 按时间新到旧列出本机审计事件，SHALL 支持按日期（--since/--until）与操作类型过滤；每条输出 SHALL 含日期时间、操作、目标与结果。

#### Scenario: 按日期过滤
- **WHEN** 用户执行 `senv audit --since <日期>`
- **THEN** 仅显示该日期（含）之后的事件，每行含日期时间

#### Scenario: 按操作类型过滤
- **WHEN** 用户执行 `senv audit --type sync`
- **THEN** 仅显示同步类事件

### Requirement: tui 查看
TUI SHALL 提供审计 Tab：按时间新到旧浏览本机事件（含日期时间、操作、目标、结果），支持操作类型过滤与翻页；无事件时明确提示。

#### Scenario: tui 浏览审计
- **WHEN** 用户切换到审计 Tab
- **THEN** 按时间新到旧展示事件列表，含日期时间与结果，可翻页
