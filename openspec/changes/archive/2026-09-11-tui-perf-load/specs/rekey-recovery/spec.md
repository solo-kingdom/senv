## ADDED Requirements

### Requirement: 读路径批量清算与 manifest 缓存

vault 读路径 SHALL 支持在单次锁获取内完成批量读取（一次清算、一次 manifest 读取、批量解密），MUST NOT 为每个条目重复获取锁与重读 rekey manifest。rekey manifest SHALL 可在进程内缓存：缓存 MUST 以 vault 元数据代际（metadata.json 的变更）为失效界，且进程内完成任何写操作或恢复动作后 MUST 立即失效。读路径的混合密钥代际隔离语义 MUST NOT 削弱：首次读取、跨进程 rekey 后的读取仍 MUST 经锁内清算，读者 MUST NOT 看见 rekey 过程中的混合代际状态。

#### Scenario: 批量读取单次锁

- **WHEN** TUI 启动全量装载含 N 个条目的 vault
- **THEN** 锁获取与清算次数为常数级（而非 N 次），耗时日志可见装载耗时显著低于逐文件基线

#### Scenario: 本进程写后缓存失效

- **WHEN** 同一进程内完成一次写操作或 rekey 动作后再次批量读取
- **THEN** manifest 缓存已失效并重新加载，读取结果与逐文件路径一致

#### Scenario: 跨进程 rekey 后读取安全

- **WHEN** 另一进程完成 rekey 后，本进程（含缓存的 manifest）发起读取
- **THEN** 读取按元数据代际变化重新清算，不产生混合代际读取或静默解密失败之外的错误行为
