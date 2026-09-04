## ADDED Requirements

### Requirement: rekey journal entry 必须为规范受管身份

rekey manifest 的每个 entry SHALL 声明并通过其规范受管身份校验。系统 MUST 仅接受属于已验证 env、text 或 config 布局的身份；config entry 还 MUST 与有效 config index 对应。控制文件、同步 state、锁、journal 文件、sidecar 名称、未知路径布局或仅满足通用单路径段规则的 entry MUST 在执行恢复前被拒绝。

#### Scenario: journal 指向同步 state
- **WHEN** 未完成 rekey manifest 包含指向同步 state、锁或其他控制文件的 entry
- **THEN** recovery fail closed，保留 manifest 和 sidecar，不读取、删除、替换或重命名该控制文件

#### Scenario: journal 指向未索引 config 文件
- **WHEN** manifest 中的 config entry 不对应有效 config index 的规范密文文件
- **THEN** recovery 返回需要人工恢复的诊断，不触及该候选文件

#### Scenario: 规范 entry 恢复
- **WHEN** manifest 中所有 entry 都是经验证的 env、text 或 config 身份，且 generation hash 与 metadata generation 匹配
- **THEN** 系统按既有 rollback 或 roll-forward 规则恢复完整单一 generation 并清理事务材料
