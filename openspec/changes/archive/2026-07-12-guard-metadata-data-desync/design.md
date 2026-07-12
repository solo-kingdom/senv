## Context

v0.1.6（`ef02de0`）在 `session.Manager.GetCachedKey` 中新增了两项会话一致性检查：`cache.Salt == metadata.Salt` 与 `storage.VerifyKey(key)`。这在 metadata 与数据文件**一致**时完全正确（已实测验证）。但在多机 git 同步场景下，`metadata.json` 可能与加密数据文件（`env_*.json.enc`、`texts/`、config）脱节——典型成因：新机器 clone 后误跑 `senv init` 生成新 salt、删 metadata 后重建、merge 冲突解决不当。

当前实现的两个问题：
1. **误报**：stale 分支清缓存后回退到密码校验，而密码校验所依据的 metadata 本身已与数据脱节，于是正确密码也报 `invalid password`。
2. **破坏性清理**：stale 分支 `_ = clearCache()` 会删掉唯一能解开旧数据文件的 session key，可能导致数据不可恢复。

已用复现脚本实证：desync 状态下 v0.1.5 "能用"（复用旧 key 解数据，不碰 metadata）、v0.1.6 "密码失败"。

## Goals / Non-Goals

**Goals:**
- 区分"密码错"与"数据不同步"，给出真实原因。
- stale 处理变为非破坏性，保留旧 key 作为恢复 fallback。
- 在 init 与 git pull 两个入口防止/检测 desync 的产生。
- 提供 `senv doctor` 一键体检。

**Non-Goals:**
- 不改加密算法、密钥派生或 metadata 文件格式。
- 不实现"改密码"或批量重新加密迁移（独立变更）。
- 不自动修复脱节文件，仅检测 + 指引。

## Decisions

### D1 — 类型化错误，区分 stale 与 auth failure

在 `internal/session` 引入哨兵错误：

| 错误 | 触发条件 | 含义 |
|---|---|---|
| `ErrSessionExpired` | timeout 到期 / boot 变化 | 可安全清理，回退密码 |
| `ErrSessionStaleMetadata` | `cache.Salt != metadata.Salt` | metadata 可能被替换，**cache 可能是恢复钥匙** |
| `ErrSessionStaleKey` | salt 一致但 `VerifyKey` 失败 | 内部矛盾，需诊断 |
| `ErrNoSession` | 无 cache | 正常回退密码 |

`internal/storage` 引入 `ErrDataDesync`，供 cmd 层判断"是否数据层问题"。

**替代方案**：用 `errors.Is` + 包装字符串。否决——可发现性差，cmd 层难精准分支。

### D2 — 非破坏性 stale 处理 + 诊断探针

`GetCachedKey` 的 stale 分支 **MUST NOT** 调用 `clearCache()`。改为：
- 返回类型化错误；
- 新增 `PeekCachedKey() ([]byte, *SessionCache, error)`：返回原始 cached key + cache，**不做校验、不清缓存**，专供诊断探针使用。

缓存清理只发生在：显式 `session clear`、或 `session start` 成功后覆写。

**错误处理策略（cmd 层）**：

```
GetCachedKey 失败
  ├─ ErrNoSession / ErrSessionExpired → 正常回退密码提示
  └─ ErrSessionStale*  → 用 PeekCachedKey 拿到旧 key
        └─ storage.CheckConsistency(旧 key)
              ├─ 旧 key 能解数据文件但不能解 metadata.password_key
              │     → 报 ErrDataDesync："metadata 与数据不同步，请勿 re-init；
              │       你的 session key 仍能解开数据，建议…"
              ├─ 旧 key 谁都解不开 → 清缓存，回退密码
              └─ 旧 key 都能解（salt 仅字段漂移）→ 引导 session start 刷新
```

### D3 — 一致性探针 API（storage 层）

```go
type ConsistencyReport struct {
    MetadataKeyOK bool          // key 能否解 metadata.PasswordKey
    EnvFiles      FileProbes    // 每个 env_*.json.enc 的可解性
    TextFiles     FileProbes
    ConfigFiles   FileProbes
}
type FileProbes struct { OK, Total int; Failed []string }
func (m *Manager) CheckConsistency(key []byte) (*ConsistencyReport, error)
```

仅返回布尔/计数/文件名，**不返回任何明文**。doctor 与 cmd 诊断共用。

### D4 — init 防呆

`storage.Initialize` 增加前置检查：若 data 目录存在 `env_*.json.enc`（或 `texts/`）但 config 目录无 `metadata.json` → 返回明确错误，拒绝 init，引导用户恢复 metadata 或走重新加密流程（本变更不实现该流程，仅指路）。

### D5 — `senv doctor` 命令

```
senv doctor          # 复用 session key；无 session 则提示密码（临时认证）
```
输出人类可读报告：metadata↔env/text/config 一致性、脱节文件清单、恢复建议。

### D6 — git pull 自检

`senv git pull` 成功后，若有 session key 则跑 `CheckConsistency`；不一致时打印警告并提示运行 `senv doctor`。无 session 则跳过（避免强制提示密码）。

### 数据流图

```
                      ┌──────────────────┐
   senv <cmd> ───────▶│ GetCachedKey     │
                      └────────┬─────────┘
                               │
              ┌────────────────┼─────────────────┐
              ▼                ▼                 ▼
         OK→用 key       stale/err          no session
              │                │                 │
              │         ┌──────▼──────┐          │
              │         │PeekCachedKey│          │
              │         └──────┬──────┘          │
              │                ▼                 │
              │      ┌──────────────────┐        │
              │      │CheckConsistency()│        │
              │      └────────┬─────────┘        │
              │          ┌────┼────┐             │
              │          ▼    ▼    ▼             │
              │      desync全坏  salt漂移        │
              │          │                              │
              │          ▼                 ▼           ▼
              │    报 ErrDataDesync      回退密码提示 ◀──┘
              │    (不清缓存)              │
              │                            ├─ VerifyPassword OK → 临时认证
              │                            └─ VerifyPassword fail → 报密码错
              ▼
         正常加解密
```

## Risks / Trade-offs

- **[旧 stale cache 残留]** 不再自动清 → 用户感官"session 总失效"。→ 缓解：password 校验成功后，cmd 层清掉已知无用的 stale cache（此时 metadata 已被证明一致，清理安全）。
- **[doctor 需要解密探查]** 增加攻击面。→ 缓解：仅布尔/文件名输出，不落盘中间结果，不在日志记明文。
- **[init 防呆误拒]** 用户确实想全新开始时被挡。→ 缓解：错误信息给出 `--force` 指引（本变更不含 flag，仅文案指路；后续变更可加）。
- **[git pull 自检依赖 session]** 无 session 时不自检 → desync 可能延迟发现。→ 缓解：doctor 作为随时可用的兜底。

## Backward Compatibility

- **不改任何存储格式**：metadata.json、env_*.json.enc、session cache 结构与字段不变。
- 旧项目（metadata 与数据一致）行为完全不变；唯一行为差异：stale cache 不再被静默清掉（更安全）。
- v0.1.6 已生成的 cache 与本变更完全兼容。

## CLI 使用示例

```bash
# 体检（最常用）
senv doctor
# 输出示例：
#   metadata ↔ key:        OK
#   env files (6/7):       OK
#     ⚠ env_dev.json.enc.backup: 无法用当前 key 解密（旧密文？）
#   text files (12/12):    OK
#   config files (3/3):    OK

# init 防呆触发
senv init
# 错误：检测到 data/ 下已有 7 个加密文件但无 metadata。
#       直接 init 会生成新密钥，导致这些文件无法解密。
#       建议：从 git 恢复 metadata.json，或参考 docs 重新加密流程。

# git pull 后自动自检
senv git pull
# ⚠ 拉取完成，但检测到 metadata 与部分数据文件不同步。运行 `senv doctor` 查看详情。
```

## Migration Plan

1. 先合入 D1+D2（错误分类 + 非破坏性）——纯安全改进，零回归风险。
2. 再合入 D3+D5（探针 + doctor）——新增能力。
3. 最后 D4+D6（init 防呆 + pull 自检）——入口加固。
无需数据迁移；可按 module 分步发版。回滚：还原代码即可，无残留状态。

## Open Questions

- doctor 在无 session 且密码校验失败时，是否应允许"仅列出文件名/计数"而不解密？当前倾向：是（只统计文件存在性，不探查可解性），以便用户在 desync 时也能看到文件清单。
- `--force` init 是否纳入本变更？当前倾向：否，仅文案指路，留给后续变更。
