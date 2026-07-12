## ADDED Requirements

### Requirement: 一致性探针不泄露明文

`storage.Manager` SHALL 提供 `CheckConsistency(key)` 探查给定 key 对 `metadata.PasswordKey`、各 `env_*.json.enc`、`texts/` 与 config 文件的可解密性。返回值 MUST 仅包含布尔结果、可解/总数计数与文件名列表，MUST NOT 包含任何明文或 derived key 字节。探针过程 MUST NOT 向磁盘写入中间结果。

#### Scenario: 一致项目全部可解

- **WHEN** 对一个 metadata 与所有数据文件均一致的合法项目，用正确的 derived key 调用 `CheckConsistency`
- **THEN** 返回 `MetadataKeyOK=true`，且各类文件的 `OK==Total`，无 failed 列表

#### Scenario: desync 项目精准定位脱节文件

- **WHEN** 项目中 `env_dev.json.enc` 与 metadata 不同步，用当前 derived key 调用 `CheckConsistency`
- **THEN** 报告将 `env_dev.json.enc` 列入 failed，其余文件标记为 OK，且不输出任何明文

#### Scenario: 任意 key 长度异常时不 panic

- **WHEN** 向 `CheckConsistency` 传入长度不等于 `crypto.KeySize` 的 key
- **THEN** 系统将该 key 视为不可解（视为全部 failed），不抛出 panic、不报运行时错误

### Requirement: init 防呆避免制造 desync

`storage.Initialize` SHALL 在生成新 metadata 前检查 data 目录：若已存在任何加密数据文件（`env_*.json.enc` 或 `texts/` 下文件）但 config 目录无 `metadata.json`，系统 MUST 拒绝初始化并输出明确错误，告知用户"直接 init 会生成新密钥导致既有密文无法解密"，引导其恢复 metadata 或走重新加密流程。

#### Scenario: 有密文无 metadata 时拒绝 init

- **WHEN** data 目录存在 `env_default.json.enc` 但 config 目录无 `metadata.json`，用户运行 `senv init`
- **THEN** 系统拒绝初始化，输出包含"无法解密"与恢复指引的错误，且不创建新 metadata

#### Scenario: 全新空目录正常 init

- **WHEN** data 与 config 目录均为空（或不存在），用户运行 `senv init`
- **THEN** 系统照常生成 salt、metadata 并完成初始化

#### Scenario: 已初始化项目保持原有拒绝

- **WHEN** config 目录已存在 `metadata.json`，用户再次运行 `senv init`
- **THEN** 系统照常报"project already initialized"，行为与防呆检查互不干扰

### Requirement: senv doctor 体检命令

系统 SHALL 提供 `senv doctor` 命令，输出 `metadata` 与 env/text/config 数据文件之间的一致性报告。该命令 SHALL 优先复用有效 session 的 derived key；无有效 session 时 MAY 提示密码作临时认证（MUST NOT 写入 session）。报告 MUST 列出脱节文件清单与恢复建议，MUST NOT 输出明文或 key。

#### Scenario: 有 session 时一键体检

- **WHEN** 存在有效 session，用户运行 `senv doctor`
- **THEN** 系统不提示密码，输出 metadata↔env/text/config 的一致性计数与脱节文件列表

#### Scenario: 无 session 时临时认证体检

- **WHEN** 无有效 session，用户运行 `senv doctor` 并输入正确密码
- **THEN** 命令完成体检；随后 `senv session status` 仍显示无 active session（不落盘）

#### Scenario: desync 时给出恢复指引

- **WHEN** 项目存在 desync，用户运行 `senv doctor`
- **THEN** 报告标记脱节文件并给出恢复建议（如恢复 metadata、重新加密），不自动修改任何文件

### Requirement: git pull 后一致性自检

`senv git pull` 在拉取完成后 SHALL 进行一致性自检：若存在有效 session key，系统 SHALL 调用 `CheckConsistency` 校验拉取后的状态；检测到不一致时 MUST 打印警告并提示运行 `senv doctor`。无有效 session 时 MAY 跳过解密探查（避免强制密码提示）。

#### Scenario: pull 引入 desync 时警告

- **WHEN** 存在有效 session，`senv git pull` 拉取的改动使部分数据文件与 metadata 不同步
- **THEN** 命令输出警告，提示运行 `senv doctor` 查看详情，且不自动修改或删除文件

#### Scenario: 无 session 时跳过探查不阻塞

- **WHEN** 无有效 session，用户运行 `senv git pull`
- **THEN** 拉取正常完成，不强制提示密码，自检被跳过
