## ADDED Requirements

### Requirement: 机器本地工件不进入同步集合

server 同步的本地收集 SHALL 排除全部机器本地工件（TUI 首屏快照、同步状态快照、同步/口令锁等）。这些工件 MUST NOT 作为任何 kind 的条目参与推送、拉取、待推送计数或指纹计算。远端若已存在由旧版本误收的同名条目，系统 SHALL 在首次同步时以删除标记清理，且 MUST NOT 因此中止整个同步批次。

#### Scenario: 收集跳过机器本地工件
- **WHEN** 数据目录顶层存在 tui-snapshot.enc 与 .senv-sync-state.json
- **THEN** 收集结果中不出现对应 config 条目，待推送计数不受其内容变化影响

#### Scenario: 清理远端历史误收条目
- **WHEN** 本地收集已排除机器本地工件，但同步 state 中仍记录远端存在 config/tui-snapshot
- **THEN** 下一次 push 以删除标记清理该远端条目，其余条目正常同步

#### Scenario: 机器本地工件内容变化不产生冲突
- **WHEN** 某机器本地工件每次会话内容都变化（如快照重写）
- **THEN** 不产生待推送条目，也不触发乐观锁冲突
