## ADDED Requirements

### Requirement: rekey 跳过机器本地工件

rekey 预检遍历 SHALL 识别并跳过机器本地工件（同步/口令锁、同步状态快照、TUI 首屏快照等），MUST NOT 将其视为未索引 config 密文而中止 rekey，也 MUST NOT 对其重加密、删除或重命名。只有非机器本地、且不被有效 config index 引用的顶层密文文件才 MUST 触发失败关闭。

#### Scenario: 存在 TUI 快照时 rekey 正常
- **WHEN** 数据目录顶层存在 tui-snapshot.enc 且 config index 不引用它
- **THEN** rekey 预检通过，不报 unindexed 错误，快照内容与位置保持不变

#### Scenario: 真正的未索引密文仍失败关闭
- **WHEN** 数据目录顶层存在既非机器本地工件、也不被 config index 引用的密文文件
- **THEN** rekey 在写入任何密文或 metadata 前失败，不切换 metadata
