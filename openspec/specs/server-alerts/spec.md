# server-alerts Specification

## Purpose
server 将安全事件流中的异常模式（连续认证失败、屏蔽事件、新设备注册、client 换 IP、单次批量删除）主动推送到管理员配置的通用 webhook，无需人工翻查访问日志即可发现异常。
## Requirements
### Requirement: 通用 webhook 告警

server SHALL 支持通过环境变量 `SENV_SERVER_ALERT_WEBHOOK`（或等效 flag）配置一个 webhook URL；未配置时告警功能 MUST 完全关闭且对请求处理零开销。检测到异常事件时 SHALL 异步 POST JSON（事件类型、时间、来源 IP、client/user 标识）。投递 MUST NOT 发生在请求处理路径内；payload MUST NOT 含 token、口令或密文内容。

#### Scenario: 未配置时零开销
- **WHEN** serve 未配置 webhook URL
- **THEN** 告警不启动，请求处理行为与现状完全一致

#### Scenario: 连续爆破告警
- **WHEN** 同一来源连续 AUTH-FAILED 超阈值
- **THEN** 向 webhook POST 一条 auth_fail_storm 告警，含来源 IP 与次数

#### Scenario: 批量删除告警
- **WHEN** 单次 push 的 tombstone 数达到 `--alert-mass-delete-threshold`（默认 50，0 关闭）
- **THEN** 向 webhook POST 一条 mass_delete 告警，`count` 为本次删除条数，带 client 标识（无 client 时带来源 IP）

#### Scenario: 请求路径不受投递影响
- **WHEN** webhook 端点不可达
- **THEN** 后台重试有限次数后丢弃并记录服务端日志，API 响应不受影响

### Requirement: 告警去抖

同一 (告警类型, 来源) 在最小通知间隔窗口内 MUST NOT 重复通知；窗口内重复事件仅累计计数。

#### Scenario: 告警风暴只通知一次
- **WHEN** 同一 IP 的 AUTH-FAILED 在窗口内持续发生
- **THEN** 该窗口内只收到一条告警，其后事件不重复推送

### Requirement: 可配置告警请求体模板

server SHALL 支持通过环境变量 `SENV_SERVER_ALERT_BODY_TEMPLATE`（或等效 flag `--alert-body-template`）配置告警请求体模板；未配置时 MUST 保持默认行为——POST 检测器自身的 JSON payload。模板以告警事件字段为变量（`alert`/`time`/`ip`/`user_id`/`client_id`/`user`/`client`/`reason`/`count`），缺席字段 MUST 渲染为空值而非模板占位残留。渲染失败 MUST NOT 影响请求处理路径：模板无法解析时 SHALL 回退默认 JSON payload 并记录日志；渲染期错误（如引用未知字段）SHALL 丢弃该条告警并记录日志。渲染结果 MUST NOT 含 token、口令或密文。

#### Scenario: 模板渲染成 provider 专有格式

- **WHEN** 配置 `{"msg_type":"text","content":{"text":"通知｜{{.alert}} ip={{.ip}} count={{.count}}"}}` 形态的模板且有事件触发
- **THEN** POST body 为该模板渲染结果，含事件元数据与 provider 所需外层结构

#### Scenario: 未配置模板时行为不变

- **WHEN** 未配置 body 模板
- **THEN** POST body 仍是默认 JSON payload，与引入模板能力前逐字段一致

#### Scenario: 模板语法错误回退默认

- **WHEN** 配置的模板无法解析
- **THEN** 记录一条日志，告警改按默认 JSON payload 投递，服务不中断

#### Scenario: 模板引用未知字段丢弃该条

- **WHEN** 模板引用了不存在的字段导致渲染失败
- **THEN** 该条告警不投递并记录日志，请求路径与其他告警不受影响

### Requirement: 投递结果按响应内容判定

webhook 返回 HTTP 2xx MUST NOT 单独作为投递成功的依据：响应体为 JSON 且含 `code`/`errcode`/`error_code` 任一非零值时 SHALL 判定为投递失败并进入重试；响应体非 JSON 或不含上述字段时 SHALL 判定为成功（通用网关语义）。重试耗尽后的丢弃日志 MUST 带状态码与响应体片段，以便区分「网关不可达」与「网关收下了但拒绝」。

#### Scenario: 200 带非零错误码视为失败

- **WHEN** webhook 返回 200 且 body 为 `{"code":19024,"msg":"Key Words Not Found"}`
- **THEN** 该次投递判定为失败并重试，重试耗尽后日志记录状态码与该 body 片段

#### Scenario: 普通网关 2xx 视为成功

- **WHEN** webhook 返回 200 且 body 为 `ok` 或 `{}`
- **THEN** 投递判定为成功，不再重试

