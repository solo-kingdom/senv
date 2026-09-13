## MODIFIED Requirements

### Requirement: 加密与同步不变性

host/keypair 数据 SHALL 以密文进入 vault，并复用现有 git/server 同步通道（零知识，仅见密文）。server 模式下 host/keypair 以 `ssh_host` / `ssh_keypair` kind 经 syncschema 白名单双向分发，密文落回本机 `hosts/`、`keypairs/` 收集目录；git 模式随 vault 目录整体分发，不经白名单。client 与 server 的白名单来自同一 syncschema 包：新 client 对旧 server push 携带 SSH 条目的批次时，server 事务前整批校验 SHALL 拒绝（发布顺序约束见 ADR-0020）。

#### Scenario: Sync contains ciphertext only

- **WHEN** 执行 push/pull（git 或 server 模式）
- **THEN** 同步载体中 SHALL 只包含加密后的 host/keypair 数据

#### Scenario: server 模式白名单双向分发

- **WHEN** 已升级的 client 与 server 之间执行增量同步
- **THEN** 本机 `hosts/`、`keypairs/` 的档案密文进入待推送集合，远端 SSH 条目 pull 后落回原目录，`senv ssh host list` / `senv keypair list` 可见
