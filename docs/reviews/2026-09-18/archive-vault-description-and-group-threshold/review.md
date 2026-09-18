# Review: archive-vault-description-and-group-threshold

**Change:** `chore/archive-vault-description-and-group-threshold` @ `c22de23`
**Repo:** `solo-kingdom/senv` (Go 1.24, cobra CLI, AES-256-GCM, PBKDF2)
**Diff base:** `origin/main` (1 commit, 97 files, +2127 / -300)
**Review scope:** 9 核心生产文件，1139 行 diff
  - `internal/storage/{description,manager,types}.go`
  - `internal/{env,text,llm,config,conflict}/.../*.go`
  - `internal/ssh/host.go`
**Reviewer:** MiniMax-M3 via token-api.wii.pub/v1 (manual fallback；OCR CLI 与 MiniMax 工具调用不兼容)
**Date:** 2026-09-18

> ⚠️ **review method 异常** —— OCR CLI 反复在 `file_read` 工具上死循环（MiniMax-M3 生成反向行范围 `start_line > end_line`），多次重试 M2.5/M2.7/M2.7-highspeed/M3 + `effort=low` + `--max-tokens-budget` + `--timeout` 仍超时失败。回退为直接调用 MiniMax chat completions 对核心 diff 做结构化 review，并已对每条 finding 二次人工核对（grep 原文验证）后再定级。

---

## P0（发布阻断 / 安全 / 数据损坏）

无。

---

## P1（应尽快修的缺陷）

### 1. `internal/env/manager.go` — `GetWithMeta` 在非 `os.IsNotExist` 错误上吞掉原 error

**位置：** `GetWithMeta`，约 147–172 行
**事实：** 调用 `m.storage.LoadEnvVarWithKey(group, key, cryptoKey)` 拿到 `err` 后，仅在 `os.IsNotExist(err)` 分支处理；紧接着的 `else` 块又走了一次 `loadEnvGroup(group)` fallback，**丢弃了原始 error**（解密失败 / 鉴权失败 / I/O 错误会被旧格式数据掩盖）。
**Why：** 新格式文件的解密或解析失败会被静默替换为旧格式数据，可能返回陈旧值，并把 vault 损坏隐藏在「读到了」的背后。
**Fix：** 在 `else` 分支保留原 `err`（`fmt.Errorf("...: %w", err)`）并返回，或仅当 `os.IsNotExist` 时才走 fallback。

### 2. `internal/storage/manager.go` — `SaveEnvGroupWithKey` 覆盖每条 entry 的 `CreatedAt/UpdatedAt`

**位置：** `SaveEnvGroupWithKey` 内的 entry 构造
**事实：** 该函数对 group 内每个 `EnvVarEntry` 直接写入 group 级别的 `CreatedAt/UpdatedAt`，覆盖真实的 per-file 时间戳。
**Why：** 任何 batch save（import、迁移、未来的批处理调用方）会把所有变量历史时间戳塌缩成 group 级时间，破坏审计保真度与依赖 per-entry mtime 的 UI/list。
**Fix：** 先逐文件 Load 保留原 `CreatedAt/UpdatedAt` 再合并，或在签名上接受现有 `EnvVarEntry` 透传。

---

## P2（普通缺陷）

### 3. `internal/text/manager.go` — `EnsureGroup` 是 TOCTOU

**位置：** `EnsureGroup`（约 511 行）：`TextGroupExists` 在 `withVaultRead` 内检查，再调 `AddGroup` 重新拿 mutation 锁。
**Why：** 两个并发调用方（或 `DeleteGroup` 并发）都能观测到「不存在」并同时尝试 `AddGroup`，可能丢失 description 或产生 race。
**Fix：** 把存在性检查与创建放进同一个 `m.mutate` 调用内，或让 `AddGroup` 自身对已存在 group 幂等。

### 4. `internal/text/manager.go` — `AddGroup` 创建目录后失败留下「无 meta 的孤儿目录」

**位置：** `AddGroup`（约 485–510）：先 `AddTextGroup` 创建目录，再 `resolveCryptoKey` / `SaveTextGroupMetaWithKey`。
**Why：** 如果后续步骤失败，目录存在但 `.meta.enc` 缺失；后续 `Set` 见到 `requireGroup` 通过，但 group 没有元数据，无恢复路径。
**Fix：** 先 resolve crypto key（或接受为参数）再触碰文件系统；失败时回滚目录。

### 5. `internal/storage/manager.go` — `Initialize` 顺序失败无 rollback

**位置：** `Initialize`（约 188–213）：env `default` 先创建，text `default` 再 `llm-keys` 顺序创建；任一步失败留下半初始化的 vault。
**Why：** 用户首次初始化遇到瞬时错误会进入代码不预期的「半状态」，需手动修复。
**Fix：** 先把 crypto key 派生并把所有 group 准备就绪再 commit filesystem；或在 text 失败时回滚已创建的 env/text 目录。

### 6. `internal/storage/manager.go` — `Initialize` 每次都覆盖 `llm-keys` 描述

**位置：** `Initialize` 末尾的 `SaveTextGroupMetaWithKey("llm-keys", llmMeta, ...)`（约 211 行）
**Why：** 用硬编码 `"reserved: LLM API keys (CLI/TUI only)"` 无条件覆盖；用户对保留组 description 的修改在每次 init/repair 时被吞掉。
**Fix：** 仅在没有 `.meta.enc` 时 seed reserved description，行为对齐 `EnsureGroup` 而非 save。

### 7. `internal/env/manager.go` — `SaveEnvGroupMetaWithKey` 失败留下「无 meta 的 group 目录」

**位置：** `SaveEnvGroupMetaWithKey`（约 757 行）：`EnsureDir` 后 `AtomicWrite`，失败时新目录已存在但无 `.meta.enc`。
**Why：** 后续 `Set` 见到 `EnvGroupExists=true` 且 `LoadEnvGroupMetaWithKey` 返回 `(nil,nil)`，隐式迁移不触发，写入成功但 group 永远无 metadata。
**Fix：** `AtomicWrite` 失败时删掉新建目录（或先写临时文件，成功后 rename）。

### 8. `internal/env/manager.go` — `RenameGroup` 丢失 legacy group 的 description

**位置：** `RenameGroup`（约 720–744 行）：用 `envGroup.Description` 构造 `EnvGroupMeta` 写回。
**Why：** legacy single-JSON 格式的 group 在 load 时不读 description，得到的 `envGroup.Description` 为空字符串；rename 时把空值写进 `.meta.enc`，用户「原本」的任何 note 静默丢失。
**Fix：** rename 时若检测到无 `.meta.enc`，先写一个合成的 `.meta.enc`（带迁移标记）保留 name/created-at；或 surface warning 说明 metadata 丢失。

---

## P3（低影响仍值得修）

### 9. `internal/llm/provider.go` — `ensureLLMKeysGroup` 在 password-mode 每次重新派生 PBKDF2

**位置：** `ensureLLMKeysGroup`（约 80 行）→ `textManager()` → `text.NewManager(password)` → `AddGroup` → `resolveCryptoKey`
**Why：** AddProvider / EditProvider 每次带 API key 都会走此路径。`m.key` 模式下走 `m.key` 字段 O(1) 返回，但 `m.password` 模式下每次 `text.NewManager` 是新实例、`resolveCryptoKey` 重复 PBKDF2（10万次迭代）。**实际 P3 而非 LLM 最初报告的 P1。**
**Fix：** 在 `ProviderManager` 持有 `text.Manager` 缓存（带派生 key 字段），按 lock-by-mutate 复用；或注入已派生的 key bytes。

---

## 已复核并判定为「失实」的发现

| 原始 LLM 结论 | 复核结果 |
|---|---|
| F8: KeyPair `Edit` / `Import` 缺 `ValidateDescription` | **失实** — `internal/ssh/manager.go:120`（Import）和 `:347`（Edit）均已校验 |
| F9: `entry.ValidateLLMProvider` 不含 description 校验，可能旁路上限 | **失实（误报等级）** — `internal/llm/provider.go:163/:417` 在 `Save` 前已独立 `ValidateDescription`；`entry.ValidateLLMProvider` 不重复校验是设计选择 |
| F0: listGroupsInfo/Snapshot 内 N×PBKDF2 | **降为 P2** — `resolveCryptoKey` 在 `m.key != nil` 时 O(1)；仅 password-mode 派生 |

---

## 重点建议（先做 P1）

1. 修 `GetWithMeta` 的 else 分支吞错（1 处）
2. 修 `SaveEnvGroupWithKey` 时间戳塌缩（1 处）
3. 这两处都是「安静地破坏数据正确性」，先于合并或发布前修

P2 的 5 条建议在 1 个集中提交里统一修：rollback / EnsureGroup 收敛 / Initialize 的 `llm-keys` 幂等。
P3 的 LLM 路径 PBKDF2 优化是性能非正确性，可单独排期。
