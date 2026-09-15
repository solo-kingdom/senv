## Context

见 proposal.md — Why。现状：

- `cmd/env.go` 在 `Export()` 后对整段 shell 脚本调用 `resolveValue(..., loose=false)`，任一缺失引用即非 0。
- `cmd/mcp.go` 的 `senv_env_export` 同样严格。
- `cmd/mcp_export.go` 已对 MCP 档案 export 注入 `ResolveLoose`（2026-09 跨机同步 change）。
- `RenameProvider` 迁移 `llm-keys/<old>` → `<new>` 但不改 env 里 codex `ensureCodexEnvName` 写入的 SeedRef。
- `internal/ref` 已具备 `ResolveWithWarnings` + `Loose` + `PrintWarnings`。

## Goals / Non-Goals

**Goals:**

- shell 主路径 `eval "$(senv env export --if-session)"` 在部分 stale 引用下仍能注入其余变量。
- provider rename 后消除典型 dangling `{{text:llm-keys:<old>}}`。
- CLI / MCP / 行为与 spec 一致；warning 可 grep、含 env key。

**Non-Goals:**

- 通用 text/env `RenameKey` 引用重写。
- MCP server profile env 模板级联。
- rename 时同步改 env key 名（如 `SENV_OLD` → `SENV_NEW`）；codex 仍靠重跑 switch 更新 `env_key`。

## Decisions

### D1：export 缺失目标走 loose，结构性错误仍 strict

**选择**：复用 `ref.ResolveOptions{Loose: true}`；循环引用、超深度仍返回 error → 命令非 0。

**备选**：新增 export 专用 resolver 分支 — 与 MCP export 重复，拒绝。

### D2：逐条变量 resolve，再拼 export 行

**选择**：在 `cmd/env.go`（或抽出 `internal/env/export_resolve.go`）对 `Export()` 返回前的 `map[key]value` 逐条 `ResolveWithWarnings`，warning 前缀 `warning: env export: <KEY>:`。

**备选**：整段字符串一次 resolve — warning 无法标注 env key，拒绝。

**数据流：**

```
Export() → map[key]value
    ↓ 逐 key
ResolveWithWarnings(value, getter, {Loose:true})
    ↓ warnings → stderr (PrintWarnings 或包装)
    ↓ resolved value
fmt export KEY='...' → stdout
```

### D3：MCP `senv_env_export` 共用同一 helper

**选择**：从 `cmd/text.go` 抽出 `resolveExportValues(envMgr, textMgr) (shell string, warnings, error)`，CLI 与 MCP 共用。

**备选**：MCP 单独实现 — 易漂移，拒绝。

### D4：RenameProvider env 级联 — 精确 SeedRef 匹配

**选择**：在 `ProviderManager.RenameProvider` 的 `mutate` 内，步骤 2 成功后：

1. `oldRef := "{{text:llm-keys:" + oldAlias + "}}"`
2. `newRef := "{{text:llm-keys:" + newAlias + "}}"`
3. `envMgr.Snapshot()` 遍历全部分组；`value == oldRef` 时 `Set(group, key, newRef)`
4. 计数写入 `RenameProviderResult.EnvRefsUpdated`

**备选**：子串替换 — 可能误伤复合模板，拒绝（spec 要求精确匹配）。

**备选**：同时 rename env key 为 `senvEnvKeyName(new)` — 需改 codex 未 switch 时的语义，超出本 change。

**错误处理**：env 改写任一步失败 → mutate 整体回滚（与 text rename 回滚 credential 同模式）。

### D5：存量脏数据

rename 级联只防未来；已 stale 的数据靠 D1 export loose 解堵。用户可手动 `senv env set` 或重跑 switch。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| eval 注入含 `{{...}}` 字面量的 env | 与 MCP export / `--loose` 一致；warning 引导修复 |
| inactive 组 stale ref rename 后仍不 export | 级联仍更新值；activate 后即正常 |
| Snapshot + 多次 Set 性能 | rename 低频；全 vault 扫描可接受 |
| spec 与 Strict mode (default) 看似冲突 | spec 明确 export 为例外路径 |

## Migration Plan

1. 部署新版本后 `env export` 立即容错，无需数据迁移。
2. 用户侧：看到 warning 后 `senv env list` 定位 key，或 `senv ai provider rename` 自动修复（仅 rename 后新操作）。
3. 回滚：恢复旧二进制 → export 再次 strict；rename 级联写入的 env 值仍有效（指向新 text key）。

## Open Questions

（无 — env key 是否随 rename 同步已在 Non-Goals 明确 defer。）

## 命令示例

```bash
# 部分 stale 时仍能 eval，stderr 见 warning
eval "$(senv env export --if-session)" 2>>~/.log/senv/export-warn.log

# rename 后 env SeedRef 自动更新
senv ai provider rename TokenApi main
# 输出含：已更新 1 条 env 引用
```
