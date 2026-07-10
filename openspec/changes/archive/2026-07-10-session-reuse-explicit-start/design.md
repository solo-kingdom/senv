## Context

当前鉴权分裂：`env`/`text` 会读 session cache；`config`/`tui`/`interactive` 总是要密码。后三者（以及 env/text 在 cache miss 后）还会按 `settings.session.timeout`（默认 `8h`）隐式 `StartSession`，覆盖用户显式 `session start -t never` 写下的持久 cache。TUI 无法复用 session 的直接原因是 `config.Manager` 与 storage config API 只有 password 路径，没有 `WithKey`。

## Goals / Non-Goals

**Goals:**

- 凡需解密的入口：有效 session → 一律用 derived key，免密
- 无 session：功能内输密码只服务当前进程，不写/不刷新 cache
- 仅 `senv session start` 负责创建/更新 session
- config 补齐 key 路径，使 TUI/config/interactive 与 env/text 对齐

**Non-Goals:**

- 不改 settings 默认 `8h`
- 不改 timeout 类型与 cache 路径策略
- 不在 cache 中存明文密码
- 不改加密格式

## Decisions

### 1. 统一鉴权策略：session 优先，密码临时

```
GetCachedKey()
    │
    ├─ ok   → NewManagerWithKey(...)  → 执行命令 / 进 TUI
    │
    └─ miss → promptPassword
                 → NewManager(password)  → 仅本次使用
                 → 不调用 StartSession
```

**替代方案：** miss 时仍按 settings 自动开 session（现状）→ 拒绝，会覆盖 never。  
**替代方案：** miss 时询问「是否保存 session」→ 超出本次范围，显式 `session start` 已足够。

### 2. 隐式 StartSession 全部移除

从以下路径删除「密码验证成功后 `StartSession(settings.timeout)`」：

- `cmd/env.go` `getEnvManager`
- `cmd/text.go` `getTextManager`
- `cmd/tui.go` `getManagersAt`
- `cmd/interactive_main.go` `runInteractive`

保留：`cmd/session.go` `session start`。

### 3. config 对齐 env/text 的 key 路径

- `storage`: `LoadConfigFileWithKey` / `SaveConfigFileWithKey`（password 版改为 derive 后委托，与 env/text 同构）
- `config.Manager`: 增加 `key []byte` + `NewManagerWithKey`；load/save 分支与 env 一致

无存储格式变更，仅 API 扩展。

### 4. TUI / interactive / config CLI 共用同一鉴权语义

`getManagers` / `getConfigManager` / `runInteractive` 均：先 `GetCachedKey`，失败再密码且不落盘。

## 数据流

```
┌─────────────────┐     有效      ┌──────────────────┐
│ session cache   │──────────────▶│ ManagerWithKey   │──▶ 业务
│ (仅 session     │               └──────────────────┘
│  start 写入)    │
└────────┬────────┘
         │ 无效/不存在
         ▼
┌─────────────────┐  临时密码   ┌──────────────────┐
│ promptPassword  │────────────▶│ Manager(password)│──▶ 业务
└─────────────────┘             └──────────────────┘
         │
         ✗ 不写 cache
```

## 错误处理

| 情况 | 行为 |
|------|------|
| 未初始化 | 现有错误，不进 TUI / 不执行 |
| 无 session + 密码错误 | 现有 invalid password，退出 |
| 无 session + 密码正确 | 本次成功；cache 仍为空（`session status` 仍无 active） |
| 有 session + data path mismatch | 视为无效，回落到密码临时认证 |
| `session start` 失败 | 仅影响该命令，不波及功能命令 |

## 使用示例

```bash
# 显式开长期 session（唯一落盘方式）
senv session start -t never

# 此后任意入口免密
senv tui
senv env list
senv config list

# 未 start 时：用一次要一次密码，且不留下 session
senv env get FOO          # 要密码
senv session status       # 仍无 active session
```

## Risks / Trade-offs

- [无 session 时每次功能都要密码，体验变「烦」] → 缓解：文档强调先 `session start`；默认 settings 仍启用 session，用户习惯显式 start 即可
- [BREAKING：依赖「跑一次 env 就自动开 8h session」的脚本会失效] → 缓解：改为先 `session start`；在 proposal/文档标明行为变更
- [config WithKey 漏改某条 save 路径导致仍走空 password] → 缓解：与 env 同构分支 + 单测覆盖 key 读写

## Migration Plan

1. 发布说明：功能命令不再隐式开 session；请用 `senv session start`
2. 无需数据迁移；旧 cache 文件格式不变
3. 回滚：恢复隐式 `StartSession` 即可（不推荐）

## Open Questions

无（settings 默认不改为 never；临时密码不落盘 — 已由产品确认）
