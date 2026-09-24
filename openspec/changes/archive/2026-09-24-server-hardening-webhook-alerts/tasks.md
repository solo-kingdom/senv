## 1. 告警检测器

- [x] 1.1 handler 新增 alert detector：订阅 recordAccess 事件流（channel 投递，buffer 满丢弃 + slog）
- [x] 1.2 四类规则：连续 AUTH-FAILED 阈值（默认 10，flag --alert-auth-fail-threshold）、BLOCKED、注册成功、client 换 IP（启动时从 access_log 回放初始化 last_ip map，增量纯内存）
- [x] 1.3 去抖：(alert_type, ip|client) 最小通知间隔（默认 5 分钟，flag --alert-debounce）

## 2. 投递

- [x] 2.1 goroutine 消费 channel，POST JSON（类型/时间/IP/client/user 名）；失败指数退避重试 ≤3 次后丢弃 + slog
- [x] 2.2 main.go：env SENV_SERVER_ALERT_WEBHOOK + flag --alert-webhook；空 = 告警关闭且 detector 不启动

## 3. 收尾

- [x] 3.1 测试：未配置零开销回归；四类事件各触发一次；去抖风暴；webhook 挂掉不影响请求
- [x] 3.2 `.agents/skills/senv-cli/SKILL.md` 同步新 flag；help 验证（go run . --help / <command> --help / mcp list-tools）
