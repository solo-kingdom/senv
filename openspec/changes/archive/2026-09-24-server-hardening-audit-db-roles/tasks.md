## 1. 审计写入

- [x] 1.1 store：`AccessOutcomeAdmin` 常量加入枚举校验；`RecordAccess` 放行
- [x] 1.2 main.go：五个 admin 子命令（create-user / revoke-token / create-registration / block-client / unblock-client）成功后调用 store 写 ADMIN 事件（reason 编码操作类型与目标，user_id/client_id 填目标对象），失败仅 slog
- [x] 1.3 `admin logs --outcome ADMIN` 查询兼容（含测试）；既有 outcome 过滤回归

## 2. 角色隔离契约

- [x] 2.1 产出受限角色 GRANT 模板（serve 角色：表读写按现状减 access_log 的 UPDATE/DELETE；附 admin 角色所需权限清单），落位为单一事实源（建议 `senv-server/` 内 SQL 或 docs 代码块，deploy-docs 切片引用）
- [x] 2.2 行为测试：以受限角色连接时 INSERT/SELECT 成功、UPDATE/DELETE 被拒绝（testdb 集成测试，用超户先建好两角色）

## 3. 收尾

- [x] 3.1 `go test ./internal/server/...` 通过；`go run . mcp list-tools` 等 help 验证无回归
- [x] 3.2 若 admin 命令输出有变化，同步 `.agents/skills/senv-cli/SKILL.md`
