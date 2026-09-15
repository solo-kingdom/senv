## MODIFIED Requirements

### Requirement: 重命名 LLM Provider 档案
系统 SHALL 提供 `senv ai provider rename <old> <new>`。新别名校验 SHALL 与 `add` 一致（非空、无控制字符等既有 vault 身份规则）。目标别名已存在时 MUST 拒绝且不做任何写入。旧别名不存在时 MUST 以非 0 退出。成功时 SHALL 在同一次逻辑操作内完成：

1. 将档案主键从 `<old>` 改为 `<new>`（内容除 alias / `credential_ref` / 时间戳外 MUST 保持不变）；
2. 若档案凭据为规范自有引用 `text:llm-keys/<old>`：将 text 条目改名为 `llm-keys/<new>`（值不变），并把档案 `credential_ref` 更新为 `text:llm-keys/<new>`；目标 `llm-keys/<new>` 已存在时 MUST 拒绝整次 rename；自有凭据条目缺失时 SHALL 仍完成档案改名，并将 `credential_ref` 更新为新规范引用（与 remove 对缺失自有凭据的宽松一致）；
3. 若凭据为外部引用（非 `text:llm-keys/<old>`）：MUST NOT 移动或删除外部条目，档案 `credential_ref` 保持不变；
4. 本机 agent 指针文件中所有 `provider == <old>` 的条目 SHALL 更新为 `<new>`；模型集与默认模型字段保持不变；
5. 若步骤 2 改写了自有凭据 text 键名：SHALL 扫描 env 全部分组，将值**精确等于** `{{text:llm-keys/<old>}}` 的条目改写为 `{{text:llm-keys/<new>}}`（只改引用模板，不改 env key 名、不改其它值）；任一条目改写失败 MUST 回滚整次 rename。

任一步失败 MUST NOT 留下「档案已改名但凭据未迁」「凭据已迁但 env 引用仍指向旧 text 键」或「凭据已迁但档案仍旧名」的可观察中间态。命令输出 SHALL 包含受影响的 agent 指针数量与已更新的 env 引用条目数，并提示 coding agent 原生配置仍使用 `senv-<old>` 标识，需重跑 `senv ai switch` 才会写入 `senv-<new>`。rename MUST NOT 改写任何 coding agent 原生配置文件或 catalog 文件。输出与审计 MUST NOT 含凭据明文。TUI SHALL 提供等价操作。

#### Scenario: 重命名并联动自有凭据与指针
- **WHEN** 档案 `acme` 凭据为 `text:llm-keys/acme`，且本机有 2 个 agent 指针指向 `acme`，执行 `senv ai provider rename acme acme-prod`
- **THEN** 档案别名为 `acme-prod`，凭据条目为 `text:llm-keys/acme-prod` 且值不变，两处指针的 provider 为 `acme-prod`，输出提示 2 个指针已更新并提醒重跑 switch；agent 原生配置文件内容不变

#### Scenario: 重命名级联 env SeedRef
- **WHEN** 档案 `TokenApi` 凭据为 `text:llm-keys/TokenApi`，default 组存在 `SENV_TOKENAPI_API_KEY={{text:llm-keys:TokenApi}}`（由 codex switch 写入），执行 rename 到 `main`
- **THEN** 凭据变为 `text:llm-keys/main`，该 env 条目值变为 `{{text:llm-keys:main}}`，env key 名仍为 `SENV_TOKENAPI_API_KEY`，输出说明已更新 1 条 env 引用

#### Scenario: 外部引用不移动凭据
- **WHEN** 档案 `acme` 凭据为 `env:llm/KEY`，执行 rename 到 `acme-prod`
- **THEN** 档案改名成功，`credential_ref` 仍为 `env:llm/KEY`，`env:llm/KEY` 条目保留，不创建或删除 `llm-keys/*`，不扫描改写 env 引用

#### Scenario: 目标别名冲突
- **WHEN** 目标别名已存在
- **THEN** 命令非 0 退出，原档案、凭据、env 引用与指针全部不变

#### Scenario: 目标自有凭据键冲突
- **WHEN** 档案为自有凭据且 vault 中已存在 `text:llm-keys/<new>`（与源档案无关）
- **THEN** 命令非 0 退出，原档案与源自有凭据不变

#### Scenario: 自有凭据缺失仍可改名
- **WHEN** 档案引用 `text:llm-keys/acme` 但该 text 条目已不存在，执行 rename 到 `acme-prod`
- **THEN** 档案改名成功，`credential_ref` 变为 `text:llm-keys/acme-prod`，不因凭据缺失而失败；若 env 中存在值等于 `{{text:llm-keys/acme}}` 的条目，SHALL 改写为 `{{text:llm-keys/acme-prod}}`

#### Scenario: 旧别名不存在
- **WHEN** rename 的旧别名不在 vault
- **THEN** 命令非 0 退出且不做任何写入

#### Scenario: env 引用非精确 SeedRef 时不改写
- **WHEN** default 组存在 `NOTE=prefix {{text:llm-keys/acme}} suffix`，执行 rename `acme` → `acme-prod` 且自有凭据迁移
- **THEN** 该 env 条目值保持不变（非精确匹配），输出 env 引用更新数为 0
