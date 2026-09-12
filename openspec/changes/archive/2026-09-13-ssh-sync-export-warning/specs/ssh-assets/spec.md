## MODIFIED Requirements

### Requirement: 导出 OpenSSH config 片段
`senv host export` SHALL 生成 OpenSSH config 片段：默认全部 host，`--host <alias>` 可过滤；核心字段映射为 `Host`/`HostName`/`User`/`Port`/`ProxyJump`，额外 KV 直传为 OpenSSH 关键字，`IdentityFile` 统一指向 `~/.ssh/senv/<keypair name>`。`proxyJump` 引用缺失时 MUST 报错。`IdentityFile` 引用的 keypair 不在本机 vault 时，export SHALL 逐条输出 warning（指明 host 别名与缺失的 keypair 名）并照常生成片段——悬空引用由后续 materialize 或同步补齐收敛，不阻断导出。

#### Scenario: Export all hosts
- **WHEN** 执行 `senv host export`
- **THEN** 输出 SHALL 为每个 host 生成 `Host <alias>` 块，含 HostName/User/Port/ProxyJump/IdentityFile 与额外 KV

#### Scenario: Export single host
- **WHEN** 执行 `senv host export --host web`
- **THEN** 输出 SHALL 仅包含 host `web` 的块

#### Scenario: Export with dangling proxyJump
- **WHEN** 某 host 的 `proxyJump` 指向已不存在的 alias
- **THEN** `export` SHALL 报错并指出该 host

#### Scenario: IdentityFile 引用的 keypair 本机缺失
- **WHEN** 某 host 的 `identityKey` 指向本机 vault 不存在的 keypair（如档案尚未同步到本机）
- **THEN** `export` SHALL 输出逐条 warning 指明该 host 别名与缺失 keypair 名，片段照常生成且该 host 块仍含 `IdentityFile` 路径

#### Scenario: keypair 全部在位时无新增输出
- **WHEN** 所有被引用 keypair 均在本机 vault
- **THEN** `export` 输出 SHALL 与既有行为一致，不产生 warning
