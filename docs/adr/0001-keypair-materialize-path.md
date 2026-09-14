# 0001-keypair-materialize-path

KeyPair 落盘约定为 `~/.ssh/senv/`（目录 0700、文件 0600、文件名 = keypair 名），`host export` 生成的 `IdentityFile` 统一指向该路径。该约定会随导出的 ssh config 固化到用户机器上，事后更改默认值会破坏既有导出配置；放在 `~/.ssh/` 下是刻意贴近 OpenSSH 的权限模型与生态预期，而非 XDG 数据目录，属有意选择。

## Status

superseded by ADR-0023（扁平「文件名 = keypair 名」布局被分组目录布局 `keys/<组>/<名>` 取代；「留在 `~/.ssh/` 下贴近 OpenSSH 权限模型」的理由不变）
