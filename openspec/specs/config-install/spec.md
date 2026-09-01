# config-install Specification

## Purpose
把加密存储的配置安装到其 meta 声明的保存位置：先产出操作计划（dry-run），确认后执行；目标文件内容有变化时先备份，父目录缺失时递归创建。
## Requirements
### Requirement: 安装计划（dry-run）
install SHALL 先生成并展示操作计划，逐条列出：配置名、解析后的目标路径、动作（create / skip / backup+overwrite）及原因。获得确认后 SHALL 才执行；`--dry-run` SHALL 只展示计划不执行。

#### Scenario: 展示计划后确认执行
- **WHEN** 执行 install 且计划非空
- **THEN** 先输出完整计划，用户确认后才写入文件

#### Scenario: dry-run 不落盘
- **WHEN** 以 `--dry-run` 执行 install
- **THEN** 只输出计划，文件系统无任何变化

### Requirement: 安装范围
install SHALL 支持三种范围：单条配置（按 name）、单分组（按 group）、全部配置。范围内不存在的 name/group SHALL 报错且不产生任何写操作。

#### Scenario: 按分组安装
- **WHEN** 指定 `--group` 安装
- **THEN** 计划与执行仅覆盖该分组下的配置

### Requirement: 变更校验与备份
执行单条安装时：目标文件不存在 SHALL 创建（动作 create）；目标内容与解密内容字节相同 SHALL 跳过（动作 skip）；不同 SHALL 先把现有目标文件复制为同目录备份再覆盖（动作 backup+overwrite），备份文件名为 `<target>.senv-backup-<时间戳>`。

#### Scenario: 内容不同先备份
- **WHEN** 目标文件已存在且内容与解密内容不同
- **THEN** 先生成备份文件再覆盖，计划中标注 backup+overwrite

#### Scenario: 内容相同跳过
- **WHEN** 目标文件内容与解密内容字节相同
- **THEN** 不写入、不备份，计划中标注 skip

### Requirement: 父目录递归创建
目标路径的父目录不存在时 SHALL 递归创建（含多级缺失），目录权限 0755。

#### Scenario: 多级目录缺失
- **WHEN** 目标路径为 `~/.a/b/c/config.yaml` 且 `~/.a` 不存在
- **THEN** 执行时递归创建全部缺失目录后写入文件

### Requirement: 路径展开失败
目标路径展开失败（如引用未定义环境变量）时 SHALL 在计划阶段标注该条错误，执行阶段跳过该条并报告。

#### Scenario: 未定义变量
- **WHEN** 某配置的 target path 引用未设置的环境变量
- **THEN** 计划中该条标注错误，确认执行时跳过该条，其余正常执行

