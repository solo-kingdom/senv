## Why

v0.1.6 给 `GetCachedKey` 加了严格的会话一致性检查（`cache.Salt == metadata.Salt` + `VerifyKey`），方向正确。但当 `metadata.json` 与加密数据文件（`env_*.json.enc` / `texts/` / `config`）因多机 git 同步而**不同步**时（例如新机器 clone 后误跑 `senv init`、删 metadata 后重建、merge 冲突解决不当），用户会看到误导性的 `invalid password`——密码明明没错。更糟的是，stale 分支会 `_ = clearCache()` 直接清掉唯一能解开旧数据的 session key，可能导致数据无法恢复。已用复现脚本实证此机制。

## What Changes

- **诊断优先于误报**：session cache 与 metadata 不一致时，SHALL 报告真实原因（metadata/data desync），MUST NOT 笼统报 `invalid password`。
- **非破坏性清理**：在确认存在可用的恢复路径（密码可校验、或数据可解）之前，`GetCachedKey` 的 stale 分支 MUST NOT 清除 session cache。
- **init 防呆**：`senv init` 检测到 data 目录已有加密数据文件但无对应 metadata 时，SHALL 拒绝并引导用户走"重新加密"而非"重新 init"。
- **新增 `senv doctor` 命令**：体检 metadata ↔ env/text/config 一致性，列出脱节的文件。
- **git pull 自检**：`senv git pull` 完成后校验一致性，不一致时警告并给出恢复指引。
- **BREAKING**：无（错误信息与新增命令；init 在已存在数据时本来就会报错，现在是更早、更明确的拒绝）。

## Non-goals

- 不改加密算法、密钥派生（PBKDF2/AES-256-GCM）或 metadata 格式。
- 不实现"改密码"或批量重新加密迁移工具（独立变更）。
- 不自动修复/重写脱节的数据文件——只做检测 + 明确指引。
- 不改变 `session start` 的写入语义。

## Capabilities

### New Capabilities
- `data-consistency`: 检测与防止 `metadata.json` 与加密数据文件之间的密钥不同步：init 防呆、`senv doctor` 体检、git pull 后自检。

### Modified Capabilities
- `session-auth`: 当 session cache 与 metadata 不一致时，错误处理 SHALL 区分"密码错"与"数据不同步"，且 MUST NOT 在无恢复路径时破坏性清缓存。

## Security Analysis

- 错误信息与 doctor 输出 MUST NOT 泄露明文或 derived key；仅报告"能否用当前 key 解密"的布尔结果与文件名。
- doctor / 一致性检查 MUST 以 0600 权限读写临时状态，不落盘中间结果。
- init 防呆减少"误重建 metadata 致旧密文不可解"的静默数据丢失风险（安全增益）。
- 非破坏性清缓存保留旧 key 作为 fallback，仅在用户显式 `session clear` 时彻底删除。

## Impact

- `internal/session/manager.go` — `GetCachedKey` 错误分类与延迟清缓存。
- `internal/storage/manager.go` — 一致性校验 API（`CheckConsistency`）、`Initialize` 防呆。
- `cmd/init.go`、新增 `cmd/doctor.go`、`cmd/git.go`（pull 后自检）。
- `cmd/{env,tui,config,text,interactive_main}.go` — 透传 desync 诊断错误。
- 新增单测覆盖 desync 场景（已在探索阶段用复现脚本验证机制）。
