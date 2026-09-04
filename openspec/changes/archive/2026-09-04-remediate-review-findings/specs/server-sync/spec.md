## ADDED Requirements

### Requirement: 同步 apply 失败必须报告恢复结果

客户端对已验证 pull 批次的本地 apply SHALL 将被替换、删除或状态更新的缓存条目视为一个可恢复批次。任一前向写入失败时，系统 MUST 尝试恢复每个已变更条目的完整旧内容和同步 state；任一恢复步骤失败时，返回错误 MUST 明确表明 rollback 未完成，且 MUST NOT 将缓存描述为已完整回滚或推进 revision。

#### Scenario: 前向写入失败且恢复成功
- **WHEN** pull apply 在写入后续条目或同步 state 时失败，且所有已变更条目均可恢复
- **THEN** 命令失败，所有已触及缓存条目与同步 state 恢复为操作前内容，revision 不前进

#### Scenario: 恢复写入失败
- **WHEN** pull apply 的前向操作失败，且恢复某个已变更缓存条目时发生 I/O、目录或原子写入错误
- **THEN** 命令返回包含 rollback failure 的可诊断错误，不宣称旧缓存完整，且同步 state 不前进

#### Scenario: 恢复父目录失败
- **WHEN** 恢复被删除条目所需的父目录无法安全创建或访问
- **THEN** 命令返回恢复失败错误，不静默忽略该失败，也不继续提交同步 state
