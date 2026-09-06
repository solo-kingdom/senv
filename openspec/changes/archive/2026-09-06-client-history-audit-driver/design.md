## Context
grill 已收敛：`grill.md` D1–D7 全部 settled、无未决问题，术语已沉淀至仓库根 `CONTEXT.md`。driver 无代码变更，只编排 4 个子 change；本设计只定切片、顺序与依赖，实现细节下沉到各子 change 的 design.md。

## Goals / Non-Goals
**Goals:** 按功能闭环切成 4 个可独立 apply/validate 的子 change；固定实施顺序；保证跨子 change 依赖被顺序满足
**Non-Goals:** 任何实现层决策（见各子 change design）；修改 driver 协议

## Decisions
1. **按功能闭环切片**（对应原始需求 0–3）：
   - `client-history-audit-identity`：client 注册 + 屏蔽/解封 + client 感知清理（D1–D3）
   - `client-history-audit-entry-history`：条目历史 + cli/tui 查看 + 单条目恢复（D4）
   - `client-history-audit-op-audit`：本机操作审计 + cli/tui 查看（D5）
   - `client-history-audit-access-log`：server 安全日志落库 + admin 查询/清理（D6）
2. **实施顺序**：identity → entry-history → op-audit → access-log。仅 access-log 依赖 identity（clients 表提供 client 标识），其余相互独立；顺序同时让「协议新增 403 语义」的 server 端最早落地。
3. **capability 均为新增**：server-clients / server-history / operation-audit / access-log，不修改既有 requirement（`server-auth` 的 401 语义仅覆盖无效/缺失/吊销，屏蔽 403 是新增行为，无冲突）。
4. **已记录假设**：存量 user 级 token 保留（tokens.client_id 可空），不可屏蔽、只可吊销（identity design 决策 2）；审计与系统日志均为 best-effort 写入。

## 数据流（子 change 依赖）
```
identity ──┬──▶ access-log（client 标识）
           └──（相互独立）entry-history、op-audit
```

## Risks / Trade-offs
- [4 个子 change 同仓串行 apply，中途状态不一致] → 每个 apply 完成即 `validate --strict` 并保持可构建；driver 对应 checkbox 才勾选
- [403 屏蔽语义需双端配合] → identity 子 change 内保证旧 client 对新 server 行为不变（403 映射失败按普通错误处理，不误清缓存），server 先行发布
- [server 需迁移窗口] → 0002–0004 均为加表加列的向后兼容迁移，逐子 change 独立发布

## Open Questions
无
