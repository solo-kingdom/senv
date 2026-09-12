## MODIFIED Requirements

### Requirement: 明文落盘提示

导出计划 MUST 标注哪些条目会把明文值（`env`、`url`、`headers`）写入哪个文件。写入内容中的 `env`、`url` 与 header 值 SHALL 为导出时解析后的明文；任一引用无法解析时 SHALL 保留模板原文写入，MUST NOT 终止该 agent 的写入，并 SHALL 把每个无法解析的引用（含缺失的 env/text 条目名）逐条列入 warning 输出到 stderr。

#### Scenario: 计划标注明文

- **WHEN** 某 remote 档案含 headers 且目标 agent 将被写入
- **THEN** 计划中该条目标注会写入明文（含 headers）及其目标路径

#### Scenario: 引用解析失败

- **WHEN** 档案 `url` 引用 `{{env:secrets:MISSING}}` 且该 key 不存在
- **THEN** 该 agent 的目标文件仍被写入，`url` 值保留模板原文 `{{env:secrets:MISSING}}`，stderr 输出含 `env:secrets:MISSING` 的 warning，计划将该条目标注为含未解析引用

#### Scenario: 引用可解析时结果不变

- **WHEN** 档案所有引用均可解析
- **THEN** 导出结果与既有行为一致（解析为明文，无 warning）
