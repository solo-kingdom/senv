# tui-startup Specification

## Purpose
定义 senv TUI 的启动性能契约：首屏数据渲染时延上限、tab 数据懒加载时机、磁盘快照缓存的读写与安全要求，保证「本地数据优先展示」的承诺可验证。

## Requirements

### Requirement: 后台网络同步不得阻塞首屏本地数据渲染
后台 sync pull 的网络请求 SHALL NOT 在 vault 排它锁内执行；网络请求与本地数据读取 SHALL 可并行。首屏本地数据的可见性 MUST NOT 依赖网络请求的完成。

#### Scenario: pull 网络缓慢时首屏仍出现
- **WHEN** 启动 TUI 且服务端响应超过 throttle 预算
- **THEN** 首屏在本地解密完成时限内渲染出已有数据，不被 pull 等待拖累

#### Scenario: 多 tab 并发加载与 pull 并行
- **WHEN** Init 触发多个 tab 数据加载且后台 pull 同时进行
- **THEN** 各 tab 加载与 pull 网络阶段不互相在锁上串行排队

### Requirement: text 域读取不得逐条目获取排它锁
text 域的列表/快照读取 SHALL 在一次锁持有内完成全部密文读取与解密，不得对每个条目单独获取排它锁。读取结果的条目内容与解密正确性 MUST 与原有逐条读取一致。

#### Scenario: 多 text 条目加载
- **WHEN** text vault 含 N 个条目且 TUI 加载 text 数据
- **THEN** 排它锁获取次数不随 N 线性增长，且展示内容与原行为一致

### Requirement: 非聚焦 tab 数据在首次聚焦前不加载
TUI 启动时 SHALL 只加载当前聚焦 tab 及全局共享数据（搜索索引、快照）；其余 tab SHALL 在其首次获得聚焦时才开始加载。已加载 tab 的数据在切换离开后 MUST 保持可用（重聚焦不得重新全量加载）。

#### Scenario: 启动只加载当前 tab
- **WHEN** 用户启动 TUI 且从未切换到 config tab
- **THEN** config 数据未被读取；首次聚焦 config tab 时出现加载，之后切回 env 再切到 config 无二次加载

#### Scenario: 全局搜索可用性不受懒加载影响
- **WHEN** 用户在任何 tab 发起全局搜索
- **THEN** 搜索结果覆盖全部已解密域，不要求先遍历打开各 tab

### Requirement: 加密快照缓存加速冷启动首屏
系统 SHALL 在本地维护一份以 vault 主密钥加密（AES-256-GCM）的明文列表快照，文件权限为 0600、目录 0700。启动时 SHALL 先解密快照并立即渲染；后台完成真实解密后 MUST 校验快照与真实数据一致，不一致时以真实数据替换展示。快照缺失、损坏或无法解密时 MUST 静默回退到原有直接解密路径。

#### Scenario: 快照命中时首屏即时
- **WHEN** 存在与当前 vault 一致的快照
- **THEN** 首屏在快照解密时限内渲染，不等待全量 vault 解密

#### Scenario: 快照过期或损坏
- **WHEN** 快照校验失败、文件损坏或权限异常
- **THEN** 回退到直接解密路径，不报错、不渲染错误内容

#### Scenario: 快照不含明文泄露
- **WHEN** 检查快照文件内容
- **THEN** 文件内不出现任何明文条目内容，仅以 vault 主密钥加密存储
