## MODIFIED Requirements

### Requirement: 冲突检测与人工解决

push 返回冲突时，系统 MUST 向用户列出冲突条目标识与两侧可用的非机密对比信息（本地 base revision、远端 current revision、删除状态、密文大小、hash、可用更新时间），MUST NOT 自动覆盖任一端数据。非交互环境 MUST 保留现有解决指引；交互环境 MUST 在应用前展示覆盖计划并要求显式确认。新增 server 字段缺失时，CLI SHALL 显示可用信息并保持冲突语义。

#### Scenario: 非交互冲突报告

- **WHEN** 在非 TTY 或指定 `--no-interactive` 时同步遇到冲突
- **THEN** 输出条目标识、本地与远端可用的 revision / size / 状态 / 时间信息，以及既有 `--accept-remote` / `--force-push` 指引，且不修改任何一端数据

#### Scenario: 交互冲突概览

- **WHEN** 在 TTY 中执行 `senv sync` 且未指定冲突策略时遇到冲突
- **THEN** 进入冲突解决器，默认显示脱敏摘要，用户可选择查看、合并或解决条目，确认前不写入本地或远端

#### Scenario: 写冲突

- **WHEN** 本地修改的条目在远端已被更新，执行同步
- **THEN** 同步中止并列出冲突条目及两侧可用对比信息，两端数据均保持不变

## ADDED Requirements

### Requirement: 交互式冲突内容对比

交互式冲突解决器 SHALL 允许用户逐条查看本地与远端内容对比。明文查看 MUST 仅在终端可交互且已获得可用 vault key 后发生；无有效 key 时，系统 MAY 在用户明确请求查看时提示输入 vault 口令。未能解密的一侧 SHALL 保持脱敏摘要，MUST NOT 导致另一侧数据被修改。

#### Scenario: 使用会话密钥查看明文

- **WHEN** 用户在冲突解决器中请求查看内容且存在可用会话密钥
- **THEN** 系统按条目类型显示本地与远端的安全对比，环境变量值默认掩码并仅在显式揭示时显示

#### Scenario: 无法解密远端内容

- **WHEN** 远端 metadata 与本地 key 不兼容且用户未提供可解密远端的凭据
- **THEN** 明细视图显示 key 兼容性警告和可用非机密信息，不显示错误解密结果，也不改变冲突状态

### Requirement: 可选 editor 手动合并

交互式冲突解决器 SHALL 为 `text`、UTF-8 `config`、`env` 与 `config_index` 提供可选 editor 手动合并。合并缓冲区 MUST 标明 LOCAL 与 REMOTE 内容；最终结果 MUST 通过类型校验且不残留冲突标记后才可应用。系统 MUST 在应用前暂存并确认结果，并以冲突时远端 current revision 作为推送 base；若远端在此期间再次变化，MUST 重新检测冲突且不应用已过期的合并结果。

#### Scenario: 手动合并文本条目

- **WHEN** 用户对 `text` 冲突选择 editor 合并并保存合法最终文本
- **THEN** 系统显示合并结果摘要，确认后原子更新本地待推送内容并基于远端 current revision 推送

#### Scenario: 保存未解决标记

- **WHEN** 用户退出 editor 后合并缓冲区仍包含 LOCAL/REMOTE 冲突标记
- **THEN** 系统拒绝应用该结果，说明未解决标记，并保留原始冲突与临时前状态

#### Scenario: 不支持手动合并的类型

- **WHEN** 冲突为二进制 config、删除冲突、`env_meta` 或 vault metadata
- **THEN** editor 合并入口不可用或被拒绝，用户仍可选择本地或远端

### Requirement: 冲突解决安全边界

冲突摘要与日志 MUST NOT 输出明文内容或冲突密文。editor 合并 MUST 使用一次性私有目录与私有文件，并在会话结束后清理。vault metadata MUST NOT 提供原始内容编辑，只能显示安全摘要并整体选择本地或远端。

#### Scenario: editor 清理

- **WHEN** editor 正常退出、失败或合并校验失败
- **THEN** 系统清理本次合并使用的一次性私有目录，不把明文写入持久日志

#### Scenario: metadata 安全摘要

- **WHEN** vault metadata 两端均修改
- **THEN** 界面仅显示安全诊断信息和整体选择入口，不暴露 raw metadata blob 或密钥材料
