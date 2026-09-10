## Why

Review 发现 `agent-provider-switch` 存在可破坏配置备份、遗留半写状态、`llm_providers` 绕过 vault 生命周期，以及凭据经 argv 暴露的问题。这些问题会破坏“失败不落半写”与加密资产管理承诺，须在功能扩大使用前修复。

## What Changes

- 修正切换写回：安全恢复备份、支持多文件适配器整体回滚、按目标路径串行化、使用真实 TOML 解析，成功后清理备份。
- 将 `llm_providers` 接入 rekey、孤立数据检测、一致性检查；加载档案时复验字段；修复自有凭据的孤儿与删除顺序问题。
- **BREAKING**：移除 `senv ai provider add --api-key`；新增 TTY prompt 与 `--api-key-stdin`。base URL 默认要求 HTTPS，本地或特殊环境须显式 `--allow-http`；拒绝 URL userinfo。
- 收紧本地安全边界：拒绝含控制字符的 vault 身份，home 不可用时失败，指针/agent 配置目录权限收敛到 0700。

## Non-goals

- 不引入凭据代理，不改变 ADR 0002 已接受的“切换后目标 agent 配置持有明文 key”。
- 不改变本机指针模型、MCP 工具契约、目录缓存来源或 Codex env_key 方案。
- 不迁移历史目录缓存，不做跨机器分布式锁。

## Capabilities

### New Capabilities

- `llm-provider-vault-lifecycle`: LLM Provider 作为 vault 加密集合的生命周期与安全输入契约。

### Modified Capabilities

- `llm-provider-switch`: 强化多文件事务、回滚、并发与 TOML merge 行为。
- `llm-provider`: 更新凭据输入、URL 校验与删除/覆盖语义。

## Impact

影响 `internal/llm`、`internal/storage`、`cmd/ai*`、TUI/MCP 共用 manager，以及 Go module 可能新增 TOML 解析依赖。CLI add 凭据方式不向后兼容。

## Security Analysis

移除 argv 中的明文 API key，降低 shell history、`ps` 与 audit 泄露风险；HTTPS 默认和 userinfo 拒绝减少明文传输与凭据回显。vault 生命周期补齐可防止 rekey/init 孤立密文。目录收紧、备份清理与路径锁降低本地泄露和写竞争风险；不会将 vault 凭据发送到新增网络端点。
