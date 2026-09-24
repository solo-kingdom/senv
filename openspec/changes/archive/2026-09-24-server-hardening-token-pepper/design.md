## Context

见 proposal.md - Why 与 driver design.md 决策 3。关键点：pepper 可空回退使灰度与回退零成本；认证缓存键派生自哈希，函数替换对缓存透明。

## Goals / Non-Goals

**Goals:** 哈希函数可配置；存量兼容；轮换可观测（慢日志）。

**Non-Goals:** 强制迁移；明文重哈希。

## Decisions

1. **比对顺序**：HMAC 先查、SHA-256 回退一次（带 per-token 窗口去抖）。备选「双哈希并存两列」被否：表结构 churn 大、查询复杂度双倍。
2. **回退命中记 slow log 而非 access_log**：避免攻击者用回退路径刷日志表；slog 面向运维。
3. **pepper 读取点**：store 构造时注入（`New` 参数），不读全局 env——测试可注入、main 负责从 env 解析。

## Risks / Trade-offs

- [回退窗口期 DB 泄露仍可反查旧 token] → 文档强制要求启用 pepper 后 N 天内轮换全部 token；慢日志计数供运维核对进度
- [pepper 丢失 = 全部 token 失效] → 文档要求备份 pepper（同 DSN 凭证级别管理）
