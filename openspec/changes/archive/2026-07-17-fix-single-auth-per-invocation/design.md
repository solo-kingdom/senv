## Context

当前契约：仅 `senv session start` 写盘 session；功能命令无 cache 时临时要密码。实现上 `resolveAuth` 无进程内缓存，`env export` 路径会 `getEnvManager` → `resolveValue` → `newCombinedGetter` 再次 `getEnvManager`/`getTextManager`，无 session 时同一命令弹密码最多 3 次。zshrc 的 `eval $(senv env export)` 放大该问题：无反馈、且成功也不落盘。

约束：不恢复隐式写盘 `StartSession`；保持 desync 诊断优先于误报密码错。

## Goals / Non-Goals

**Goals:**

- 同一次 CLI 进程内鉴权至多一次成功 prompt；之后全部复用
- 非交互（非 TTY stdin）且无有效 session：禁止 prompt，stderr 明确引导 `senv session start`
- 交互临时密码仍不落盘
- 覆盖所有 `resolveAuth` 入口，避免同类重复 prompt

**Non-Goals:**

- 功能命令成功后隐式写 session cache
- 改 PBKDF2/加密格式
- 跨进程共享内存密钥（仍靠磁盘 session cache）

## Decisions

### D1 — 进程内 memoize `resolveAuth`（首选）

在 `cmd` 包用包级（或小结构体）缓存最近一次成功的 `*authResult`（按 configPath+dataPath 键）。`resolveAuth` 入口先查缓存命中则直接返回；成功鉴权后写入。失败不缓存。

```
resolveAuth
  ├─ memo hit? ──▶ return cached authResult
  ├─ GetCachedKey OK ──▶ memoize & return (key)
  ├─ stale diagnose …
  ├─ !interactive && no session ──▶ ErrNoSessionHint（stderr 文案含 session start）
  └─ prompt once → Verify → memoize & return (password)
```

**替代 A**：只改 `env export` 把 manager 往下传 —— 治标，`env get`+`resolveValue` 等路径仍会双次鉴权。  
**替代 B**：功能命令成功即 `StartSession` —— 与「仅 start 写盘」冲突，否决。

### D2 — 「交互」判定

以 `term.IsTerminal(stdin)` 为交互。非 TTY（含多数 `eval $(senv env export)` 子进程若 stdin 仍为 TTY 则仍算交互——见 D3）。

### D3 — shell `eval` 场景的可靠拒 prompt

仅靠「非 TTY」不够：`$(...)` 常继承 TTY，仍会弹密码。对 **`env export`** 增加：当 stdout 不是 TTY（典型被 command substitution 捕获）且无 session 时，同样禁止 prompt，返回可识别错误并写 stderr：

`no active session; run: senv session start`

可选旗标 `--if-session`：无 session 时静默空输出 exit 0，专供 zshrc（文档推荐）。

### D4 — 错误处理

| 情况 | 行为 |
|------|------|
| memo 命中 | 无 IO，无 prompt |
| 有有效 cache | 与现网一致，并写入 memo |
| 交互 + 无 session + 密码正确 | 临时 auth，memoize，不写盘 |
| 交互 + 密码错误 | 返回 `invalid password`，不 memoize |
| 非交互或 export+stdout 非 TTY + 无 session | 不 prompt；错误包装提示 `session start` |
| desync | 保持现有 `ErrDataDesync` 路径，不 memoize 失败态 |

### D5 — 使用示例

```bash
# 推荐 zshrc
senv session start -t never   # 登录后一次
eval "$(senv env export --if-session)"

# 无 session 时 eval（stdout 非 TTY）
# stderr: no active session; run: senv session start
# 不再弹密码

# 交互 CLI 无 session
senv env export               # 只问一次密码，引用解析不再问
senv session status           # 仍无 Active（未落盘）
```

## Risks / Trade-offs

- [Risk] 依赖 stdout-非 TTY 识别 eval → 少数把 export 重定向到文件的脚本也会拒 prompt → Mitigation：文档说明；需要临时导出时用交互终端或先 `session start`；提供 `--if-session` 给启动脚本
- [Risk] 包级 memo 在测试并行下串扰 → Mitigation：测试用独立路径键，或 `t.Cleanup` 清 memo；必要时 `sync.Once`/mutex
- [Trade-off] 无 session 的 eval 注入从「能输密码凑合用」变为「必须先 start」→ 与提案 BREAKING 一致，换清晰工作流

## Migration Plan

1. 发版说明：无 session 时 `eval $(senv env export)` 不再要密码，需先 `session start` 或改用 `--if-session`
2. 更新 README / SESSION_USAGE 示例
3. 回滚：去掉 memo 与 export 非 TTY 短路即可（不推荐）

## Open Questions

- `--if-session` 是否作为本次必做（建议是，zshrc 友好）；若只做错误退出也可，文档写 `senv session status` 判断
