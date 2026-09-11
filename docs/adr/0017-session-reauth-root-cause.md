# 会话自动清理仅限到期，多缓存新者优先

把「用户重新输入密码」当作严重体验问题后，两个判定边界被收紧：

1. **自动清理只允许发生在到期**。`GetCachedKey` 仅在判定为 `ReasonExpired`（`duration` 会话的 `expires_at` 已过）时清缓存；`ReasonRestarted` / `ReasonVaultChanged`（失效）一律保留缓存并返回带根因的错误。理由是失效缓存的 data path hash 可能属于另一个 vault，删除它等于删掉那个 vault 的唯一恢复钥匙——这正是 ADR-0010 要避免的「多 vault 互相踩」，只是从覆写换成了删除。
2. **同一槽位多份缓存按 `created_at` 新者优先**。平台安全存储与磁盘逃生舱同时命中时，不再一律返回 `errMultipleSessionCaches` 把用户挡死，而是选择更新的一份完成完整校验、保留另一份、提示选择与 `senv session clear --all`；仅当两份 `created_at` 完全相同时才报可操作错误。理由是现状的"多缓存"是升级与显式逃生舱叠加的必然产物，且 ADR-0009 已点名它是"用户无从判断"的态。

两项决策都不放松解密门槛：被选中的缓存仍须通过 data path 绑定、metadata salt、cached key 三重校验，校验不过一律拒绝，绝不放行未经验证的派生密钥。

## Considered Options

- **失效也自动清缓存（旧行为）**：重新认证最"干净"，但会删除可能是另一 vault 唯一恢复钥匙的缓存。否决。
- **多缓存保持一律报错**：实现最简单、语义最保守，但用户在升级或显式 escape hatch 后必然撞上，且无从判断该删哪份。否决。
- **多缓存固定平台存储优先**：规则简单，但逃生舱往往是用户显式创建的、可能更新，固定优先级会选到过期的那份。否决。
- **`created_at` 相同时随机或按存储类型选**：避免报错，但把不确定行为藏进选择逻辑。否决。

## Consequences

- `ErrSessionInvalidated` 不再蕴含"缓存已被清除"；cmd 层必须按根因渲染原因 + 一条确定动作，`session status` 的 `Invalidated` / `Unverifiable` 态统一显示 `Cache: retained`。
- 收紧清理后旧缓存可能长期堆积；`senv session clear --all` 与 `session status` 的提示是唯一出口，不再靠自动清理兜底。
- 多缓存选择与保留都要进审计（只含槽位标识与选中的 `created_at`，不含 key/salt/口令）。
- 无存储格式变更、无 CLI 参数变更；回滚到旧二进制仍可读全部缓存。判定三层与到期模型的其余部分见 [ADR-0009](./0009-session-expiry-model.md)，槽位身份见 [ADR-0010](./0010-vault-bound-session-identity.md)。

## Status

proposed。对应变更 `openspec/changes/reduce-reauth-surprise`。
