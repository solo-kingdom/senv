## MODIFIED Requirements

### Requirement: KeyPair 分组字段
`KeyPairEntry` SHALL 包含单值 `group` 字段（空 = 未分组），语义与 Host 的 `group` 一致：组织维度，同时参与落盘与导出 `IdentityFile` 路径推导（`~/.ssh/senv/keys/<group>/<名>`）；group 值禁止包含 `/`，写入与编辑时校验。既有数据（无 `group` 字段）SHALL 按未分组处理，读写兼容。CLI SHALL 提供 `senv keypair edit <name> --group <group>` 作为分组编辑入口（`--group` 显式变更时单字段更新、不启编辑器；空值 = 清除分组），与 TUI KeyPair Tab `e` 等价；未显式变更 `--group` 时命令 SHALL 报错拒绝执行。

#### Scenario: 既有 keypair 无 group 字段
- **WHEN** 读取旧版本写入的 keypair（无 `group` 字段）
- **THEN** 系统 SHALL 按空 group（未分组）处理，读写均正常

#### Scenario: 分组名含路径分隔符
- **WHEN** `senv keypair import web-key --file ... --group a/b` 或编辑为含 `/` 的组名
- **THEN** 系统 SHALL 报错拒绝写入

#### Scenario: CLI 编辑 keypair 分组
- **WHEN** 用户执行 `senv keypair edit web-key --group prod`
- **THEN** keypair `web-key` 的 group SHALL 更新为 `prod`，`senv keypair list` 输出展示该分组，私钥与公钥材料 MUST NOT 变动

#### Scenario: CLI 清除 keypair 分组
- **WHEN** 用户执行 `senv keypair edit web-key --group ""`
- **THEN** group SHALL 置空（未分组），后续 materialize 落盘路径按 `_ungrouped` 推导

#### Scenario: CLI 编辑为非法分组名
- **WHEN** 用户执行 `senv keypair edit web-key --group a/b`
- **THEN** 系统 SHALL 报错拒绝写入，原 group 保持不变

#### Scenario: CLI 编辑不存在的 keypair
- **WHEN** 用户执行 `senv keypair edit nope --group prod`，keypair `nope` 不存在
- **THEN** 系统 SHALL 报错且不产生任何存储变更

#### Scenario: CLI 未指定 --group
- **WHEN** 用户执行 `senv keypair edit web-key`（未显式变更 `--group`）
- **THEN** 系统 SHALL 报错提示用法，不进入编辑器、不产生任何存储变更
