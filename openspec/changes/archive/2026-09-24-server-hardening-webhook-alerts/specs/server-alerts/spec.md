# server-alerts Specification

## Purpose
server 将安全事件流中的异常模式（连续认证失败、屏蔽事件、新设备注册、client 换 IP）主动推送到管理员配置的通用 webhook，无需人工翻查访问日志即可发现异常。

## ADDED Requirements

### Requirement: 通用 webhook 告警

server SHALL 支持通过环境变量 `SENV_SERVER_ALERT_WEBHOOK`（或等效 flag）配置一个 webhook URL；未配置时告警功能 MUST 完全关闭且对请求处理零开销。检测到异常事件时 SHALL 异步 POST JSON（事件类型、时间、来源 IP、client/user 标识）。投递 MUST NOT 发生在请求处理路径内；payload MUST NOT 含 token、口令或密文内容。

#### Scenario: 未配置时零开销
- **WHEN** serve 未配置 webhook URL
- **THEN** 告警不启动，请求处理行为与现状完全一致

#### Scenario: 连续爆破告警
- **WHEN** 同一来源连续 AUTH-FAILED 超阈值
- **THEN** 向 webhook POST 一条 auth_fail_storm 告警，含来源 IP 与次数

#### Scenario: 请求路径不受投递影响
- **WHEN** webhook 端点不可达
- **THEN** 后台重试有限次数后丢弃并记录服务端日志，API 响应不受影响

### Requirement: 告警去抖

同一 (告警类型, 来源) 在最小通知间隔窗口内 MUST NOT 重复通知；窗口内重复事件仅累计计数。

#### Scenario: 告警风暴只通知一次
- **WHEN** 同一 IP 的 AUTH-FAILED 在窗口内持续发生
- **THEN** 该窗口内只收到一条告警，其后事件不重复推送
