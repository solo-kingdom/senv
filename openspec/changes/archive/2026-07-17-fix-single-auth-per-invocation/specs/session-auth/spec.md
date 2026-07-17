## ADDED Requirements

### Requirement: 单次进程内鉴权复用

在同一次 CLI 进程中，系统 SHALL 在首次成功完成鉴权（命中有效 session cache，或交互式密码校验成功）后缓存该认证结果；同一进程内后续任何需要解密的入口（含 `getEnvManager`、`getTextManager`、`getConfigManager`、`resolveValue`/`newCombinedGetter`、TUI/interactive 等）MUST 复用该结果，MUST NOT 再次提示密码。鉴权失败 MUST NOT 写入该缓存。该进程内缓存 MUST NOT 写入 session cache 文件（落盘仍仅由 `senv session start` 负责）。

#### Scenario: env export 无 session 只问一次密码

- **WHEN** 无有效 session，用户在交互式终端运行 `senv env export` 并输入一次正确密码
- **THEN** 命令成功输出 export 语句，过程中密码提示至多一次（含引用解析），且 `senv session status` 仍无 active session

#### Scenario: 同进程 resolveValue 不再二次鉴权

- **WHEN** 无有效 session，某命令已通过密码完成一次 `resolveAuth`，随后在同进程内调用 `resolveValue`（其内部需 env 与 text manager）
- **THEN** 系统不再提示密码并完成解析

#### Scenario: 鉴权失败不污染复用缓存

- **WHEN** 无有效 session，用户输入错误密码
- **THEN** 系统报告 `invalid password`，且同进程内下一次需要鉴权的调用仍可再次提示密码（未缓存失败结果）

### Requirement: 非交互或被捕获的 export 无 session 时禁止弹密码

当不存在有效 session，且满足以下任一条件时，系统 MUST NOT 提示密码，MUST 通过 stderr（或错误返回）清楚提示用户运行 `senv session start`：

1. 标准输入不是交互式终端；或
2. 命令为 `senv env export` 且标准输出不是交互式终端（例如被 `eval $(...)` 捕获）

系统 MAY 提供 `senv env export --if-session`：无有效 session 时 MUST 以空 stdout、成功退出码结束且不提示密码，供 shell 启动脚本使用。

#### Scenario: eval 捕获 export 且无 session

- **WHEN** 无有效 session，用户执行 `eval $(senv env export)`（export 的 stdout 非 TTY）
- **THEN** 系统不提示密码；stderr 含引导执行 `senv session start` 的说明；不写入 session cache

#### Scenario: export --if-session 无 session 静默跳过

- **WHEN** 无有效 session，用户运行 `senv env export --if-session`
- **THEN** stdout 为空、退出码为成功，且不提示密码

#### Scenario: 有 session 时 eval export 仍静默成功

- **WHEN** 存在有效 session，用户执行 `eval $(senv env export)`
- **THEN** 系统不提示密码并输出可 eval 的 export 语句

## MODIFIED Requirements

### Requirement: 功能内密码仅临时认证

当不存在有效 session 时，系统 MAY 在**交互式**功能命令中提示密码以完成当次操作；该密码认证 MUST NOT 创建或刷新 session cache。仅 `senv session start` SHALL 写入或更新 session cache。同一进程内首次密码成功后，后续入口 MUST 按「单次进程内鉴权复用」复用结果，MUST NOT 再次提示。非交互或被捕获的 `env export` 路径 MUST 遵守「禁止弹密码并提示 session start」的要求，不得以多次临时密码代替 session。

#### Scenario: 无 session 时 env 要密码但不落盘

- **WHEN** 无有效 session，用户在交互式终端运行 `senv env get FOO` 并输入正确密码
- **THEN** 命令成功返回值，且随后 `senv session status` 仍显示无 active session

#### Scenario: 无 session 时 TUI 要密码但不落盘

- **WHEN** 无有效 session，用户运行 `senv tui` 并输入正确密码
- **THEN** 系统进入 TUI，且不调用 session 写入；退出后 `senv session status` 仍无 active session

#### Scenario: 仅 session start 写入 cache

- **WHEN** 用户运行 `senv session start --timeout 8h` 并输入正确密码
- **THEN** 系统写入 session cache，`senv session status` 显示 Active

#### Scenario: 交互临时密码在同进程内只问一次

- **WHEN** 无有效 session，用户在交互式终端运行会触发 env 与 text 双重解析的命令（如含引用的 `env export`）并输入一次正确密码
- **THEN** 全程密码提示至多一次，命令成功，且不写入 session cache
