## Why

access_log 已落库但无人看：异常访问只能靠管理员事后翻日志发现。需要把「连续爆破、屏蔽事件、新设备注册、client 换 IP」四类事件主动推送到管理员自己的通道（n8n/飞书机器人/Telegram gateway 等通用 webhook）。编排见 `server-hardening-driver`；决策依据见其 design.md 决策 2。

实现对照：`internal/server/handler/accesslog.go`（`recordAccess` 是统一事件出口）、`senv-server/main.go`（serve flag/env 解析模式）、`internal/server/store/accesslog.go`（`AccessEvent`/`AccessEventRow`）。

## What Changes

- serve 新增可配置 webhook URL（env `SENV_SERVER_ALERT_WEBHOOK`，可选 flag `--alert-webhook` 覆盖）：为空则告警完全关闭（默认）
- 检测器订阅安全事件流：连续 AUTH-FAILED 超阈值（默认 10 次/窗口，flag 可调）、BLOCKED、注册成功（新 client）、client 换 IP 首次访问
- 告警为异步投递：事件经 channel 进 goroutine，HTTP POST JSON（含事件类型、时间、IP、client/user 名）；失败退避重试有限次数后丢弃 + slog
- 每事件类型有最小通知间隔去抖（防告警风暴，默认 5 分钟/类型/IP）

**安全性分析**：告警 payload 只含 access_log 已有的元数据，不含 token/密文；webhook URL 视为敏感配置（env/flag 不落库）。

## Non-goals

- 内置具体第三方 provider（Telegram/飞书等由用户 webhook 网关适配）
- 告警规则热更新（重启生效可接受）
- 其余三个加固项

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | senv-server：handler + main |

## 验收标准

- [ ] 未配置 webhook 时行为与现状完全一致（零开销路径回归）
- [ ] 四类事件各自触发一次 JSON POST；payload 不含 token/密文
- [ ] 去抖生效：同类型同 IP 风暴只通知一次/窗口
- [ ] webhook 不可达时请求路径不受影响（goroutine 内重试后丢弃）
- [ ] 新 flag 写入 `.agents/skills/senv-cli/SKILL.md`；`go run . mcp list-tools` 等 help 验证
