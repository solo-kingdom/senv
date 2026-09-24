## Why

四项加固的部署侧配套目前只在探索任务决策里：生产管理员需要一份「公网加固」操作手册（systemd 加固、反代配置、双 DSN cron、单实例边界、元数据泄露边界、pepper 轮换指引）。编排见 `server-hardening-driver`；决策依据见其 design.md 决策 4。

## What Changes

- `docs/senv-server.md` 新增「公网加固」节：
  - systemd 单元加固示例（DynamicUser / ProtectSystem=strict / PrivateTmp / NoNewPrivileges / RestrictAddressFamilies）
  - 反代推荐配置（HSTS、limit_req、TLS 1.2+；nginx 与 Caddy 各一最小示例）
  - 双 DSN 运维：serve 受限角色 + admin 角色 cron 跑 `logs-prune`（引用 audit-db-roles 切片的 GRANT 模板单一事实源）
  - 单实例边界声明（内存态限速器/告警去抖不支持多实例）
  - 元数据泄露边界：零知识保护内容，但 vault 名、条目数、同步频率、访问时间模式对 DB 持有者可⻅
  - pepper 启用后的轮换指引（N 天内 revoke + 重新签发；pepper 备份要求）

**安全性分析**：纯文档，无代码面变化；注意文档不含任何真实凭证/URL 样例值。

## Non-goals

- GRANT 模板本身的内容（属 audit-db-roles 切片，本文档只引用）
- senv-cli skill 正文更新（各代码切片自行同步，driver 收尾统一验证）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | docs/senv-server.md |

## 验收标准

- [ ] 「公网加固」节覆盖上述六项，GRANT 模板处引用单一事实源而非复制
- [ ] 文档命令经 `make build-server` 产物核对可执行（systemd/cron 片段除外，标注为模板）
- [ ] 交叉引用三个代码切片的锚点/flag 名与实际实现一致（实现完成后核对）
