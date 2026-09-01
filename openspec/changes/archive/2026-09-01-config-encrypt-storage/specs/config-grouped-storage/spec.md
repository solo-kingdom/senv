## Purpose
config 私密配置的分组存储模型：每条配置归属一个分组并携带 meta（描述、保存位置），内容加密存储，保存位置在使用时做 `~` 与环境变量展开。

## ADDED Requirements

### Requirement: 分组与 meta 模型
每条配置 SHALL 具有 group（分组）与 meta 信息，meta 至少包含 description（描述）与 target path（保存位置）。未指定 group 时 SHALL 落入 `default` 分组。配置 name SHALL 全局唯一，group 仅作为分类维度。

#### Scenario: 创建带分组与描述的配置
- **WHEN** 创建配置时指定 name、group、description、target path
- **THEN** 该配置以加密形式持久化，索引中记录其 group、description 与 target path 原始写法

#### Scenario: 未指定分组
- **WHEN** 创建配置时未指定 group
- **THEN** 该配置归属 `default` 分组

### Requirement: 向后兼容
旧格式索引（无 group/description 字段）SHALL 被正常读取，其中配置视为 `default` 分组、空描述，且读取操作 SHALL NOT 强制改写索引文件。

#### Scenario: 读取旧索引
- **WHEN** 加载由旧版本写入的 config 索引
- **THEN** 所有配置以 `default` 分组、空描述正常列出，无报错

### Requirement: 保存位置展开
target path 中的 `~` SHALL 展开为当前用户主目录，`$VAR` 与 `${VAR}` 形式的环境变量 SHALL 在使用时展开。展开 SHALL 发生在使用路径的操作（如 install/export）执行时，而非写入存储时。

#### Scenario: 展开 home 与变量
- **WHEN** target path 为 `~/.config/$APP_NAME/config.yaml` 且环境变量 `APP_NAME` 已设置
- **THEN** 使用时解析为主目录下对应绝对路径

#### Scenario: 引用未定义变量
- **WHEN** target path 引用的环境变量未设置
- **THEN** 操作在计划阶段报告该错误，不执行任何写操作

### Requirement: 分组查询
列表与查询操作 SHALL 支持按 group 过滤，并展示每条配置的 description 与 target path。

#### Scenario: 按组列出
- **WHEN** 按 group 列出配置
- **THEN** 仅返回该分组下的配置及其 meta 信息
