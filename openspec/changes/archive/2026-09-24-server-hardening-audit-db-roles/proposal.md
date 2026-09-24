## Why

serve 进程当前与 admin CLI 共用同一个 DSN 与数据库角色：拿到 serve DSN 即可 UPDATE/DELETE access_log，抹掉访问痕迹。admin 子命令（create-user / revoke-token / create-registration / block-client / unblock-client）执行后不留任何记录，事后无法追溯「谁在何时吊销/屏蔽了谁」。编排见 `server-hardening-driver`；决策依据见其 design.md 决策 1。

实现对照：`senv-server/main.go`（admin 子命令、DSN 解析）、`internal/server/store/accesslog.go`（`RecordAccess`、outcome 枚举、`ListAccessLogs`/`AccessLogFilter`）、`internal/server/store/store.go`（`Store` 接口）。

## What Changes

- 受限角色 GRANT 模板 + 迁移说明：serve 运行时角色对 access_log 仅 `INSERT` + `SELECT`，无 `UPDATE`/`DELETE`；`admin logs-prune` 需使用单独的高权限角色（部署文档给 cron 示例，本切片只出模板与说明）
- `access_log` outcome 枚举新增 `ADMIN` 取值（`store/accesslog.go` 常量与校验放行）
- 每个 admin 子命令成功后写一条 access_log 事件：`outcome=ADMIN`，`reason` 记录操作类型与目标（如 `create-user alice` / `revoke-token user=alice` / `block-client user=alice client=laptop`），`user_id`/`client_id` 填被操作对象；写入失败仅 slog，不影响命令结果
- `admin logs` 查询兼容 ADMIN 事件（现有 outcome 过滤即可，验证一次即可）

**安全性分析**：ADMIN 事件只含操作元数据，不含 token/密文；受限角色模板本身不改动 server 代码行为，是否采用由部署决定（缺省部署保持现状可运行，文档提示降级风险）。

## Non-goals

- serve 进程运行时强制校验自身角色（角色是部署期配置，代码无从强制——见 driver design.md 决策 1）
- cron 部署示例（属 `server-hardening-deploy-docs`）
- 其余三个加固项

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | senv-server 与 store 包 |

## 验收标准

- [ ] GRANT 模板随迁移/文档落位：受限角色对 access_log 仅 INSERT+SELECT
- [ ] outcome 枚举含 ADMIN；admin 五个子命令成功后各写一条 ADMIN 事件（含操作类型与目标），写失败不影响命令退出码
- [ ] `admin logs --outcome ADMIN` 可查；既有 outcome 过滤回归通过
- [ ] 新增行为涉及 admin 命令输出变化时同步 `.agents/skills/senv-cli/SKILL.md`
