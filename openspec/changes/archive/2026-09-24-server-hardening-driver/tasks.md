## 1. 子 change 登记（实现委托到子 change）

- [x] 1.1 子 change `server-hardening-audit-db-roles`：DB 角色隔离（serve 受限角色 + logs-prune admin 角色 cron 示例）+ admin 操作入审计（create-user / revoke-token / create-registration / block-client / unblock-client 成功后写 access_log）。设计依据见 design.md 决策 1
- [x] 1.2 子 change `server-hardening-webhook-alerts`：通用 webhook URL 告警（连续 AUTH-FAILED 超阈值、BLOCKED、新 client 注册成功、client 换 IP 首次访问；检测器走 goroutine + channel，失败丢弃 + slog）。设计依据见 design.md 决策 2
- [x] 1.3 子 change `server-hardening-token-pepper`：token 哈希改 HMAC + `SENV_SERVER_TOKEN_PEPPER` 环境变量；旧哈希回退比对路径 + 慢日志标记；轮换流程写入文档。设计依据见 design.md 决策 3
- [x] 1.4 子 change `server-hardening-deploy-docs`：`docs/senv-server.md` 新增「公网加固」节（systemd 加固、反代推荐配置、双 DSN cron、单实例边界、元数据泄露边界、pepper 启用后的轮换指引），并同步本仓既有 GRANT 模板。设计依据见 design.md 决策 4

## 2. 收尾

- [x] 2.1 四个子 change 各自 `tasks.md` 全勾且 `openspec validate --strict` 通过后，勾选本节及 proposal 验收标准对应 checkbox
- [x] 2.2 `go run . --help` / `go run . <command> --help` / `go run . mcp list-tools` 验证 senv-cli skill 同步无遗漏
