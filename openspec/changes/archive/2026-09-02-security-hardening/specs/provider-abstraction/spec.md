## ADDED Requirements

### Requirement: server provider 地址 scheme 校验

构造 server provider 时，系统 SHALL 默认仅接受 https 地址；非 https 地址 MUST 拒绝构造，错误信息 MUST 包含地址、拒绝原因与显式豁免方法。豁免（如内网 http）SHALL 通过显式环境变量开启，且开启时 MUST 在错误/日志路径保持可审计。git provider 不受此约束。

#### Scenario: http 地址默认被拒

- **WHEN** 用户将 provider 配置为 `http://example.com` 并执行任意需同步的命令
- **THEN** 构造失败，错误信息说明仅接受 https 及豁免方法，不发起任何请求

#### Scenario: 显式豁免后内网 http 可用

- **WHEN** 用户设置豁免环境变量后使用 `http://内网地址`
- **THEN** 构造成功，同步行为与 https 一致

#### Scenario: https 地址不受影响

- **WHEN** 用户使用 `https://` 的 server 地址
- **THEN** 校验通过，无需任何豁免设置
