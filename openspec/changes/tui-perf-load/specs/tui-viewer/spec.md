## ADDED Requirements

### Requirement: env 数据单趟加载与共享快照

TUI 对 vault 数据的全量消费 SHALL 通过单趟加载构建的内存快照完成：一次遍历读取每个条目的密文文件至多一次。env Tab 列表、全局搜索、AI Tab 凭据引用收集等消费方 SHALL 复用同一份快照，MUST NOT 各自重复全量遍历。写操作成功后快照 SHALL 失效并在后台单趟重建；单条读写路径（如 `senv env get`）行为不变。

#### Scenario: 启动只读每个条目一次

- **WHEN** server 模式暖启动 TUI 且远端无变更
- **THEN** env/text 等 vault 条目文件在启动装载过程中各被读取一次（以耗时日志的条目数/次数维度可验证），列表可用

#### Scenario: 写操作后快照单趟重建

- **WHEN** 用户在 TUI 内修改一个环境变量
- **THEN** 快照失效并单趟重建，期间 UI 不清空、其余条目不再重复读取

## MODIFIED Requirements

### Requirement: 后台拉取应用变更后更新展示

后台拉取应用了远端变更时，TUI SHALL NOT 将当前展示清空为加载占位：旧数据保持可见且可操作，受影响 Tab 在后台完成单趟重载后静默替换为新数据，并保留光标、过滤与表单状态。拉取无变更或零网络跳过时行为不变（只更新同步徽标）。

#### Scenario: 拉取应用变更不清空列表

- **WHEN** 用户正浏览 env 列表时后台 pull 应用了 3 条远端变更
- **THEN** 列表保持可见可操作，重载完成后条目静默更新，光标与过滤条件不丢失

#### Scenario: 拉取无变更

- **WHEN** 后台 pull 结束且未应用任何变更
- **THEN** 各 Tab 不重载，仅同步徽标更新
