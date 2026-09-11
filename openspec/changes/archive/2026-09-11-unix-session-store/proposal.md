## Why

macOS 登录钥匙串每次读写都可能弹出 GUI 授权，远程 SSH 无法点击，持久会话实际上不能用。

## What Changes

- **BREAKING**：不再把操作系统钥匙串当作安全存储或可选后端；升级不读、不删遗留 item，已有钥匙串会话失效，需重新 `session start`
- 全平台同一套 Unix 文件系统选型：能证明 tmpfs/ramfs 则写安全存储；否则写磁盘逃生舱
- stock Darwin 无法证明 memory-backed，逃生舱为默认写目标，`session start` 写入时警告；Linux 未证明时仍须显式 `--insecure-cache`
- 到期/失效仍按 ADR-0009（duration 只看 `expires_at`，restart 看 boot ID）

## Non-goals

- 不自动创建 RAM Disk，不引入凭据代理，不保留 `--keychain`
- 不改 timeout 取值、分槽身份、MCP 鉴权指纹

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `session-auth`：安全存储定义去掉钥匙串；Darwin 默认写目标改为磁盘逃生舱；停止调用钥匙串

## Impact

- 代码：`internal/session`（去掉 `keychainStore`，Darwin 走 `tmpfsStore` / 磁盘逃生舱）、`cmd/session` 帮助与警告
- 文档：README、SESSION_USAGE、RELEASE_NOTES、`.agents/skills/senv-cli/SKILL.md`
- 领域：CONTEXT.md 已改；ADR-0015 / ADR-0016

## Security Analysis

stock Darwin 上派生钥以 0600 明文落在用户磁盘，同用户进程与备份可读，换掉的是远程不可用的 GUI 阻塞。能证明 tmpfs 的路径（含自备挂载的 Darwin）仍只写内存文件系统。Linux 安全存储契约不变。钥匙串遗留 item 不再被读取，不含 vault 明文。
