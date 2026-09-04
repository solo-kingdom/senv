## ADDED Requirements

### Requirement: session start 与 vault generation 线性化

`session start` SHALL 在同一 vault generation 内完成口令验证、metadata 读取、key 派生、key 与 metadata 的匹配确认以及 cache 提交。rekey 与 session start MUST 串行化；成功返回的 session cache MUST 对应完成时的 metadata salt 和有效 derived key，不得由并发 rekey 产生已知 stale cache。

#### Scenario: rekey 先完成
- **WHEN** rekey 在 session start 获得 vault 访问权前完成
- **THEN** session start 使用新 generation 验证口令；旧口令被拒绝，或新口令成功建立可立即使用的 cache

#### Scenario: session start 先完成
- **WHEN** session start 在 rekey 前获得 vault 访问权并成功提交 cache
- **THEN** cache 与当时 metadata/key 匹配；后续 rekey 按既有撤销语义使该 cache 失效，而不是让 session start 成功返回 stale cache

### Requirement: fallback session cache 并发创建保持单一有效结果

在没有 `XDG_RUNTIME_DIR` 的已验证 memory-backed fallback 中，并发 session start SHALL 串行化 cache 创建、替换、枚举和旧目录清理。每个成功返回的 start 完成后，至少存在一个属于该 vault 的有效 session cache；清理不得删除另一个仍在建立或刚建立的有效 cache。

#### Scenario: 两个 fallback start 并发执行
- **WHEN** 两个进程同时在同一用户、同一 vault 的 fallback runtime 中启动 session
- **THEN** 两个命令按确定顺序完成，最终保留一个可验证的 session cache，且不会出现两个命令均成功但无 cache 的状态

#### Scenario: fallback 清理遇到并发 cache
- **WHEN** 一个 start 正在清理旧 fallback 目录，另一个 start 正在创建或提交 cache
- **THEN** 清理不删除正在建立或最新有效的 cache；无法安全判定时该 start 返回错误而不是删除候选目录
