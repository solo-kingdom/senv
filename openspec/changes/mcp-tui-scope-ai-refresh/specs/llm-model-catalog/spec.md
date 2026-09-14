# llm-model-catalog Delta

## ADDED Requirements

### Requirement: TUI 目录刷新入口

TUI AI Tab SHALL 提供 `R` 键从默认目录源（`https://models.dev/api.json`）联网刷新模型目录缓存：拉取并校验成功后以新缓存原子替换本地缓存（与 `senv ai refresh` 同一语义），并以 toast 展示 provider 与 model 数量摘要；任何失败（网络错误、非法内容、写盘失败）MUST NOT 改动旧缓存，SHALL 以错误提示告知原因。刷新为异步执行，MUST NOT 阻塞界面其它操作；刷新不要求 vault 解锁。`ctrl+r` 的本地档案重载语义 MUST NOT 改变（不访问网络）。

#### Scenario: TUI 成功刷新

- **WHEN** 用户在 AI Tab 按 `R` 且目录源可达、内容合法
- **THEN** 本地缓存被原子替换，toast 展示 provider 数与 model 数，随后档案列表重载

#### Scenario: TUI 刷新失败保留旧缓存

- **WHEN** 用户在 AI Tab 按 `R` 且目录源不可达、返回非法内容或写盘失败
- **THEN** 系统给出错误提示，旧缓存保持原样可继续使用，TUI 其余功能不受影响

#### Scenario: ctrl+r 保持本地重载

- **WHEN** 用户在 AI Tab 按 `ctrl+r`
- **THEN** 仅重新装载本地档案列表，不发起任何网络请求
