## ADDED Requirements

### Requirement: 机器本地工件不计入 desync 与 orphan 判定

init 防呆与一致性探针 SHALL 忽略机器本地工件。data 目录仅含机器本地工件而无 metadata 时 MUST 视为全新目录正常 init；探针 MUST NOT 将其计入失败或脱节清单。

#### Scenario: 仅剩机器本地缓存的目录可正常 init
- **WHEN** data 目录无 metadata.json，顶层只有 tui-snapshot.enc 与 .senv-sync-state.json
- **THEN** `senv init` 不报 ErrOrphanedData，照常生成 metadata 完成初始化

#### Scenario: 一致性探针忽略机器本地工件
- **WHEN** 已初始化 vault 的 data 目录同时含有受管密文与机器本地工件
- **THEN** 探针只校验受管密文，机器本地工件不出现在失败或脱节清单中
