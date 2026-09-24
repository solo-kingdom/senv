## Context

见 proposal.md - Why 与 driver design.md 决策 2。`recordAccess` 已是所有安全事件的唯一出口（认证中间件 + 注册 handler 都汇聚于此），检测器挂这里零侵入。

## Goals / Non-Goals

**Goals:** 四类事件的检测与投递；默认关闭；请求路径零阻塞。

**Non-Goals:** 规则配置化 UI；多实例下的去抖共享（单实例约束见 driver proposal）。

## Decisions

1. **检测点挂在 `recordAccess`**：事件已带 userID/clientID/reason，检测只需读行。备选「handler 内联埋点」被否：散落多处，漏挂风险。
2. **client 换 IP 检测**：以 (client_id, ip) 对查询历史——内存 map（client_id → last_ip）+ access_log 首次启动回放可选；简单起见：serve 启动时从 access_log 拉每个 client 的最近 IP 初始化 map，之后纯内存增量判断。
3. **去抖键**：(alert_type, ip|client)。窗口内重复只更新计数不重复通知。

## Risks / Trade-offs

- [webhook 端点本身成为泄露面（拿到 URL 可伪造告警）] → 文档建议网关侧加 secret 头校验；server 侧 payload 带 server 时间戳便于 freshness 校验
- [内存 map 随 client 数线性增长] → 有界：只存最近活跃 client，定期清扫（复用限速器 sweep 模式）
