# perf-log Specification

## Purpose
client 在本机记录关键路径耗时（哪个阶段/操作、花了多久、规模维度），超阈值才落盘；为性能验收与回归提供数据，与操作审计分离。
## Requirements
### Requirement: 关键路径耗时记录

client 对关键路径 SHALL 记录耗时日志：TUI 与 CLI 启动的各阶段（认证、manager/provider 构造、数据装载、首次渲染前准备）、vault 全量加载（含组数与条目数维度）、网络同步请求（pull/push，含结果与是否新建连接维度）、本地同步状态扫描（含扫描条目数维度）。单阶段耗时超过阈值时 SHALL 追加一条 JSON 行到 `~/.log/senv/perf.log`，包含阶段/操作标识、耗时、时间戳、结果（成功/失败）与规模维度。耗时日志 MUST NOT 包含任何明文值、密钥材料或凭据。日志写入失败 MUST 静默降级，不影响业务路径。

#### Scenario: TUI 暖启动各阶段超阈值

- **WHEN** 会话缓存有效时启动 `senv tui`，且「数据装载」阶段耗时 ≥ 阈值
- **THEN** `perf.log` 追加该阶段的 JSON 行，含耗时与条目规模维度；TUI 界面行为与不开启耗时日志时完全一致

#### Scenario: CLI 命令同步阶段记录

- **WHEN** 执行 `senv env list` 且 auto pull 触发了网络请求
- **THEN** `perf.log` 记录该同步阶段耗时、结果，并标注是否新建了连接

### Requirement: 阈值与开关

耗时日志 SHALL 默认启用、阈值为 100ms；用户 SHALL 可通过环境变量调高/调低阈值或整体关闭。关闭时 MUST NOT 产生任何日志写放大；阈值非法取值时 SHALL 回退默认值并照常工作。

#### Scenario: 关闭耗时日志

- **WHEN** 以关闭耗时日志的环境变量运行任意命令与 TUI
- **THEN** `perf.log` 无新增行，业务行为不变

#### Scenario: 阈值调低便于观测

- **WHEN** 以阈值 10ms 运行 `senv tui`
- **THEN** 更多阶段的耗时行被记录，格式与默认阈值一致

### Requirement: 与操作审计分离

耗时日志 SHALL 独立于操作审计：写入独立的 `~/.log/senv/perf.log`，MUST NOT 写入 `audit.log`，两类日志的查看方式互不影响。

#### Scenario: 审计文件不被污染

- **WHEN** 耗时日志记录了任意阶段
- **THEN** `audit.log` 内容与条数不因耗时记录而变化

