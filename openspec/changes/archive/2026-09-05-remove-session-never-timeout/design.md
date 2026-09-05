## Context

`never` 与 `restart` 共用同一条校验分支（`internal/session/manager.go` 的 `isCacheValid`：`TimeoutNever`/`TimeoutRestart` 均跳过到期检查、同受 boot-ID 约束），语义完全等价。`never` 的存在只产生意图标签差异和「永久留存」的误解。动机见 proposal.md。

## Goals / Non-Goals

**Goals:**

- 从解析层移除 `never`/`infinite`/`forever`，删除 `TimeoutNever` 类型常量
- 旧 `never` cache 与 settings 遗留值有清晰、可行动的失败路径
- 文档与提示文案一致：超时模型只剩 duration/restart

**Non-Goals:**

- 不引入新超时类型、不重命名 `restart`
- 不迁移/转换旧 `never` cache（见 Decisions）
- 不改动存储介质选择、MCP 守卫、审计日志结构

## Decisions

### D1: 硬移除，不做兼容别名

`ParseTimeout` 删除 `never/infinite/forever` 分支并返回明确错误（列出支持取值）。备选方案是静默映射为 `restart`——被否决：静默转换掩盖用户配置错误，违背项目「错误信息清晰明确」规范；且两者行为本就相同，映射没有信息增益。

### D2: 旧 cache 按未知类型自然过期

`isCacheValid` 的 `default` 分支已将未知 `timeout_type` 判为无效。删除 `TimeoutNever` 常量后，遗留 `"never"` cache 自动落入该分支报 `unknown timeout type`，`session status` 显示 Expired。**存储格式不变更**（JSON 结构不变，只是枚举值集合缩小），无需迁移代码；重建 session 成本为一次密码输入。备选方案是启动时改写旧 cache 的 `timeout_type` 为 `restart`——被否决：为省一次密码输入引入写路径和边界情况，过度设计。

### D3: settings 遗留值在 start 时报错

settings 的 `session.timeout` 仅在 `session start` 未传 `--timeout` 时读取。沿用现有解析路径即可：`ParseTimeout("never")` 返回的错误会带可用值提示，无需新增专门检测。功能命令不受影响（它们不解析 settings 超时）。

### D4: 测试统一改用 `restart`

所有用 `never` 验证「无到期检查」「boot-ID 失效」「MCP 保持有效」的测试改用 `restart`，断言不变。`timeout_test.go` 增加 `never` → error 的负向用例。

### D5: scenario 重命名采用 early-sync

openspec 1.8 的 delta 机制不支持在 MODIFIED 块内重命名 scenario（验证与归档均要求现有 scenario 名全部保留）。本变更将 4 处 `never` scenario 重命名（含正文）直接同步到基线 `openspec/specs/session-auth/spec.md`，delta MODIFIED 块保持目标状态；归档时这些块按「已同步」no-op 处理，`超时值校验` ADDED requirement 照常折叠。备选方案——在 delta 中保留旧 scenario 名并把正文改写为遗留 cache 语义——被否决：`never session 保持有效` 等标题与目标语义直接矛盾，会在规约中永久残留已删除概念。

## 数据流

```
session start --timeout <值>
        │
        ▼
  ParseTimeout ──"never"/"infinite"/"forever"──▶ error: invalid timeout (支持 30m/8h/1d/1y/restart)
        │ duration / restart
        ▼
  StartSession（口令验证 → 派生 key → 写 cache，timeout_type ∈ {duration, restart}）
        │
        ▼
  后续命令 GetCachedKey
        │
        ├─ timeout_type ∈ {duration, restart} 且 boot-ID/salt/key 匹配 ──▶ 有效，免密
        ├─ timeout_type 为其他值（含遗留 "never"）──▶ Expired，提示 session start
        └─ 过期/重启/换路径 ──▶ Expired（duration 到期时清缓存）
```

## 错误处理策略

| 场景 | 行为 |
|------|------|
| `--timeout never/infinite/forever` | `invalid timeout format` 错误，列出支持取值，不写 cache |
| settings `"timeout": "never"` 且未传 flag | 同上错误路径，提示修正 settings |
| 遗留 `"never"` cache | `unknown timeout type` → status 显示 Expired；命令提示重新 start，不清缓存（非破坏性约定不变） |
| 审计日志 | start 失败记 `auth_failure`；对遗留 cache 的校验失败记 `session_validate` 失败事件，格式不变 |

## CLI 使用示例

```bash
# 变更后唯一等价的无限时用法
senv session start --timeout restart   # ✓ Session started (valid until system restart)

# 被移除的用法
senv session start --timeout never
# ✗ invalid timeout format: never (supported: 30m, 8h, 1d, 1y, restart)
```

## Risks / Trade-offs

- [用户脚本/CI 中硬编码 `-t never` 升级后报错] → 错误信息直接给出 `restart` 替代；文档标注 **BREAKING**
- [settings 遗留 never 阻塞默认 start] → 报错含修正指引，属一次性迁移动作
- [审计日志出现 `unknown timeout type` 校验失败记录] → 属预期诊断信号，无需处理

## Migration Plan

无数据迁移。用户可见动作：升级后将 `never` 用法改为 `restart`（或任意 duration），重新执行一次 `session start`。

## Open Questions

（无）
