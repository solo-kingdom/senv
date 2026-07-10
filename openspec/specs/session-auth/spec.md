# session-auth Specification

## Purpose

统一 senv 各命令入口的 session 认证契约：有有效 session 则复用 derived key；无 session 时功能内密码仅作临时认证；仅 `senv session start` 写入 session cache。

## Requirements

### Requirement: 有效 session 时全入口复用

系统 SHALL 在所有需要解密的命令入口（`env`、`text`、`config`、`tui`、`interactive`）优先使用有效 session cache 中的 derived key，且 MUST NOT 再次提示密码。

#### Scenario: 有 never session 时打开 TUI

- **WHEN** 用户已执行 `senv session start --timeout never` 且 cache 有效，再运行 `senv tui`
- **THEN** 系统不提示密码并进入 TUI

#### Scenario: 有 session 时使用 config

- **WHEN** 用户存在有效 session，运行 `senv config list`（或其它 config 子命令）
- **THEN** 系统不提示密码并完成操作

#### Scenario: 有 session 时使用 interactive

- **WHEN** 用户存在有效 session，运行 `senv interactive`
- **THEN** 系统不提示密码并进入交互模式

### Requirement: 功能内密码仅临时认证

当不存在有效 session 时，系统 MAY 在功能命令中提示密码以完成当次操作；该密码认证 MUST NOT 创建或刷新 session cache。仅 `senv session start` SHALL 写入或更新 session cache。

#### Scenario: 无 session 时 env 要密码但不落盘

- **WHEN** 无有效 session，用户运行 `senv env get FOO` 并输入正确密码
- **THEN** 命令成功返回值，且随后 `senv session status` 仍显示无 active session

#### Scenario: 无 session 时 TUI 要密码但不落盘

- **WHEN** 无有效 session，用户运行 `senv tui` 并输入正确密码
- **THEN** 系统进入 TUI，且不调用 session 写入；退出后 `senv session status` 仍无 active session

#### Scenario: 仅 session start 写入 cache

- **WHEN** 用户运行 `senv session start --timeout 8h` 并输入正确密码
- **THEN** 系统写入 session cache，`senv session status` 显示 Active

### Requirement: config 支持 derived key

`config.Manager` 与 storage 的 config 读写 SHALL 支持使用 session 提供的 derived key（与 env/text 同构），以便在有效 session 下无需密码即可加解密配置文件。

#### Scenario: 用 key 读写 config

- **WHEN** 调用方使用 derived key 构造 `config.Manager` 并 load/save 某配置
- **THEN** 加解密成功，行为与使用正确密码时一致
