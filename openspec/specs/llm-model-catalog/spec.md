# llm-model-catalog Specification

## Purpose
提供 models.dev 模型目录的拉取、校验与本地缓存能力，作为 LLM Provider 添加时自动加载模型的公开数据来源，并保证离线时缓存仍可用。
## Requirements
### Requirement: 刷新拉取并缓存目录
`senv ai refresh` SHALL 从目录源（默认 `https://models.dev/api.json`，`--source` 可覆盖）拉取 provider/model 目录，校验通过后以带格式版本与拉取时间的新缓存整体替换旧缓存，并向用户输出 provider 与 model 数量摘要。

#### Scenario: 成功刷新
- **WHEN** 用户执行 `senv ai refresh` 且目录源可达、内容合法
- **THEN** 缓存被更新为新内容（含格式版本与拉取时间），命令输出 provider 数与 model 数并以 0 退出

#### Scenario: 内容非法时保留旧缓存
- **WHEN** 目录源返回的内容无法解析为合法目录（缺 provider/model、结构不符）
- **THEN** 命令以非 0 退出并给出明确错误，旧缓存保持原样可继续使用

#### Scenario: 网络失败时保留旧缓存
- **WHEN** 目录源不可达或超时
- **THEN** 命令以非 0 退出并给出明确错误，旧缓存保持原样可继续使用

### Requirement: 离线查看缓存状态
`senv ai catalog status` SHALL 在不访问网络的情况下展示当前缓存的元信息：拉取时间、目录源、provider 数与 model 数。

#### Scenario: 存在缓存时展示元信息
- **WHEN** 本地存在有效缓存且用户执行 `senv ai catalog status`
- **THEN** 命令展示拉取时间、目录源、provider 数与 model 数并以 0 退出

#### Scenario: 无缓存时提示刷新
- **WHEN** 本地不存在缓存且用户执行 `senv ai catalog status`
- **THEN** 命令以非 0 退出并提示先执行 `senv ai refresh`

### Requirement: 目录命令不依赖 vault
`senv ai refresh` 与 `senv ai catalog status` SHALL 在 vault 未初始化或未解锁的情况下照常工作。

#### Scenario: 未初始化 vault 时可刷新
- **WHEN** 用户尚未执行 `senv init` 且执行 `senv ai refresh`，目录源可达且内容合法
- **THEN** 刷新成功完成，不要求输入口令或创建 vault

