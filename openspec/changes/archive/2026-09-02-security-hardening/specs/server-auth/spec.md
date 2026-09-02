## ADDED Requirements

### Requirement: 认证失败限速

server 对认证失败的请求 SHALL 按来源实施限速（固定窗口计数即可）；超限来源的后续请求 SHALL 返回 429，且响应 MUST 与 vault 不存在时一致地不泄露任何账户或 vault 存在性信息。限速状态 MAY 保存在内存中，进程重启后清零。

#### Scenario: 连续失败触发限速

- **WHEN** 同一来源在窗口内连续提交错误 token 超过阈值
- **THEN** 后续请求返回 429，server 侧记录失败计数

#### Scenario: 正常用户不被误伤

- **WHEN** 持有效 token 的用户在窗口内正常读写（未触发失败阈值）
- **THEN** 请求正常处理，不返回 429
