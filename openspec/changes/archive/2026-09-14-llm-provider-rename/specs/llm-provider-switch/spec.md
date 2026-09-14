## ADDED Requirements

### Requirement: Provider 重命名更新本机指针
当用户执行 LLM Provider rename 时，本机 `agent-pointers` 中所有指向旧别名的条目 SHALL 将 `provider` 字段更新为新别名；Agent 模型集与默认模型 MUST 保持不变。rename MUST NOT 改写各 coding agent 的原生配置文件，也 MUST NOT 重命名或删除 `senv-<old>` catalog 等由既往 `switch` 产生的派生文件。此后 `senv ai status` SHALL 显示新别名；若原生配置仍含 `senv-<old>`，用户重跑 `senv ai switch <agent> <new>` 后 SHALL 按既有 switch 语义写出 `senv-<new>` 并清理不再被指向的旧 catalog（与缩集/换 provider 的清理规则一致）。指针文件缺失或为空时 rename 的档案/凭据步骤仍可成功，受影响指针数记为 0。

#### Scenario: 指针随 rename 更新
- **WHEN** codex 与 pi 的指针均指向 `acme`，执行 provider rename `acme` → `acme-prod`
- **THEN** 两处指针的 provider 均为 `acme-prod`，模型集与默认模型不变；codex/pi 原生配置文件内容不变

#### Scenario: 重切写出新标识并清理旧 catalog
- **WHEN** rename 后用户对仍含 `senv-acme` 的 agent 执行 `senv ai switch <agent> acme-prod`
- **THEN** 原生配置与 catalog 使用 `senv-acme-prod`；若不再有任何指针引用 `acme`，既有 switch 清理规则删除不再需要的 `senv-acme` catalog

#### Scenario: 无指针文件仍可 rename
- **WHEN** 本机尚无 agent 指针文件，执行 rename
- **THEN** 档案（及适用时的自有凭据）改名成功，受影响指针数为 0
