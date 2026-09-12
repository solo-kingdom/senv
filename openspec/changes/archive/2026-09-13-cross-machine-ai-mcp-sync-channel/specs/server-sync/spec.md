## MODIFIED Requirements

### Requirement: 同步条目标识遵守严格 schema

server 与客户端 SHALL 仅接受已知 kind，并按 kind 校验 grp/key：`env` 要求 grp/key 均为安全单路径段；`env_meta` 要求 grp 为安全单路径段且 key 为空；`text` 要求 grp/key 均为安全单路径段；`config` 要求 grp 为空且 key 为安全单路径段；`config_index` 要求 grp/key 均为空；`llm_provider` 要求 grp 为空且 key 为安全单路径段（LLM Provider alias）；`mcp_server` 要求 grp 为空且 key 为安全单路径段（MCP Server alias）。未知 kind、缺失字段、额外身份字段或不安全路径段 MUST 被拒绝。

#### Scenario: 五种合法 kind 被接受

- **WHEN** push 或 pull 条目分别满足 `env`、`env_meta`、`text`、`config`、`config_index` 的身份 schema
- **THEN** 条目继续按正常同步流程处理

#### Scenario: 配置源档案 kind 被接受

- **WHEN** push 或 pull 条目分别为 `llm_provider`、`mcp_server` 且 grp 为空、key 为安全单路径段（alias）
- **THEN** 条目继续按正常同步流程处理

#### Scenario: kind 未知

- **WHEN** 条目的 kind 不在已知白名单中
- **THEN** server 拒绝整批 push，客户端拒绝 apply，且不创建任何本地文件

#### Scenario: kind 字段组合非法

- **WHEN** `config` 携带 grp、`env_meta` 携带 key、`llm_provider` 或 `mcp_server` 携带 grp 或 key 为空，或要求的 grp/key 为空
- **THEN** 条目在访问文件系统前被拒绝

#### Scenario: 身份包含路径语义

- **WHEN** grp 或 key 为 `../x`、`a/../../x`、绝对路径、含 `/`、`\\`、NUL、`.` 或 `..`
- **THEN** server 和客户端均返回验证错误，条目不会进入持久化或本地缓存

## ADDED Requirements

### Requirement: 配置源档案随同步通道分发

人工添加进 vault 的 LLM Provider 档案与 MCP Server 档案 SHALL 作为配置源随同步通道双向分发（push 收集、pull 落盘），档案密文落回本机数据目录的 `llm_providers/`、`mcp_servers/` 收集目录。首次在新机器同步时 SHALL 默认拉取全部配置源档案，不要求显式 opt-in；既有 `--accept-remote` 重建路径保持不变。配置源条目发生冲突时，系统 SHALL 沿用与其他 kind 相同的 revision 乐观锁与冲突解决流程，且 SHALL 在冲突报告与审计输出中额外给出本地与远端的 alias 与 revision 对照提示，提醒双端可能存在有意义的人工修改。

#### Scenario: 新机器首次同步拉到配置源档案

- **WHEN** 一台未配置过 LLM Provider / MCP Server 的新机器完成首次 server 模式 pull
- **THEN** 远端 vault 中的全部 `llm_provider` / `mcp_server` 档案密文出现在本机对应收集目录，`senv ai provider list` 与 `senv mcp list` 可见

#### Scenario: 配置源档案本地修改后推送

- **WHEN** 用户在本机 `senv ai provider add` 或 `senv mcp add` 一条新档案后执行 push
- **THEN** 该档案以 `llm_provider` / `mcp_server` kind 进入待推送批次并成功上传，远端 revision 递增

#### Scenario: 配置源档案冲突时的对照提示

- **WHEN** 两台机器基于同一 revision 分别修改同一 alias 的档案并推送
- **THEN** 后推送一方收到与其他 kind 一致的冲突处理流程，且冲突报告与审计输出额外包含该条目的本地/远端 alias 与 revision 对照 warning

#### Scenario: 档案密文按既有加密姿态落盘

- **WHEN** pull 应用一条 `llm_provider` / `mcp_server` 档案
- **THEN** 落盘内容为与其他 kind 一致的加密 blob（明文不出机），文件与目录权限与既有收集目录一致
