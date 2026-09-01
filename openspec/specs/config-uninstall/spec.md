# config-uninstall Specification

## Purpose
把配置从其保存位置移除：先产出操作计划（dry-run），确认后执行；目标文件未被本地改动时直接删除，被改动时必须显式确认。
## Requirements
### Requirement: 卸载计划（dry-run）
uninstall SHALL 先生成并展示操作计划，逐条列出：配置名、目标路径、动作（delete / noop / changed）及原因。获得确认后 SHALL 才执行；`--dry-run` SHALL 只展示计划不执行。

#### Scenario: 展示计划后确认执行
- **WHEN** 执行 uninstall 且计划非空
- **THEN** 先输出完整计划，用户确认后才删除文件

### Requirement: 卸载范围
uninstall SHALL 支持三种范围：单条配置（按 name）、单分组（按 group）、全部配置。

#### Scenario: 按分组卸载
- **WHEN** 指定 `--group` 卸载
- **THEN** 计划与执行仅覆盖该分组下配置的安装位置文件

### Requirement: 改动检测与确认
执行单条卸载时：目标文件不存在 SHALL 视为已完成（动作 noop）；目标内容与解密内容字节相同 SHALL 直接删除（动作 delete）；不同 SHALL 标记为 changed，只有用户对该条目显式确认后才删除，否则跳过。

#### Scenario: 未改动直接删除
- **WHEN** 目标文件内容与解密内容字节相同且用户确认计划
- **THEN** 删除目标文件

#### Scenario: 已改动需二次确认
- **WHEN** 目标文件内容与解密内容不同
- **THEN** 计划中标注 changed，执行前对该条目单独确认，用户拒绝则保留文件

### Requirement: 存储本体不受影响
uninstall SHALL NOT 删除或修改加密存储中的配置数据与索引。

#### Scenario: 卸载后可重新安装
- **WHEN** uninstall 删除目标文件后再执行 install
- **THEN** 配置从加密存储正常恢复

