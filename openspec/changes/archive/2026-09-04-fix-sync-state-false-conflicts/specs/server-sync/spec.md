## MODIFIED Requirements

### Requirement: 冲突检测与人工解决

push 返回冲突时，系统 SHALL 先区分"假冲突"与"真实冲突"：本地快照缺失（条目被视为新增）或快照哈希失配，但本地与远端密文字节一致时，MUST 自动收养远端 revision / 哈希进本地同步状态并继续同步，MUST NOT 报冲突、MUST NOT 改动两端数据。仅当两端密文确实不同时，MUST 向用户列出冲突条目标识，MUST NOT 自动覆盖任一端数据，MUST 给出解决指引（如拉取后重新编辑或强制覆盖的命令）。真实内容冲突 v1 SHALL 只检测不自动合并。

#### Scenario: 快照缺失导致的新增误判被自动收养

- **WHEN** 同步状态丢失某条目快照，但该条目本地文件与远端密文一致，执行 senv sync
- **THEN** 同步自动将该条目远端 revision 写入本地快照，同步成功完成且不报冲突，两端数据字节不变

#### Scenario: metadata 快照哈希为空但两端一致

- **WHEN** 同步状态 metadata 哈希为空，本地与远端 metadata blob 一致，执行 senv sync
- **THEN** 同步自动收养远端 metadata 哈希，同步成功完成且不报 metadata 冲突

#### Scenario: 写冲突

- **WHEN** 本地修改的条目在远端已被更新且两端密文不同，执行同步
- **THEN** 同步中止并列出冲突条目，两端数据均保持不变

## ADDED Requirements

### Requirement: 同步状态防退化校验

持久化同步状态时，系统 MUST 拒绝写入相对现有状态出现退化的内容：entries 数量减少但不存在对应删除标记，或 metadata 哈希从非空变为空。拒绝时 MUST 返回明确错误并保留现有状态文件不变。

#### Scenario: 拦截快照丢失的写入

- **WHEN** 某次同步试图保存比现有状态少一个条目且无对应 tombstone 的状态
- **THEN** 保存被拒绝并报错，磁盘上的状态文件保持原有完整内容

#### Scenario: 拦截 metadata 哈希清空

- **WHEN** 某次同步试图将非空 metadata 哈希覆盖为空串
- **THEN** 保存被拒绝并报错，磁盘上的状态文件保持原有完整内容

### Requirement: 同步状态 vault 绑定与写入来源

同步状态 SHALL 记录其所属 vault 绑定（server 地址指纹与 vault 名）及最近一次写入来源（代码路径、进程 pid、时间）。加载时若绑定与当前配置不符，MUST 拒绝复用该状态并提示用户执行重建；来源字段 MUST NOT 包含敏感内容。

#### Scenario: 拒绝复用其他 vault 的状态

- **WHEN** 同一本地缓存目录被切换到另一个 vault 后执行同步
- **THEN** 同步因状态文件 vault 绑定不符而中止，并提示执行状态重建，MUST NOT 静默沿用旧快照

#### Scenario: 旧版本状态文件兼容

- **WHEN** 加载缺少绑定字段的旧状态文件且当前 vault 与历史一致
- **THEN** 正常加载并在下一次成功写入时补全绑定与来源字段
