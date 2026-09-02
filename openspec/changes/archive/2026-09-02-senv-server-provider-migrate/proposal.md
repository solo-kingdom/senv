## Why

driver 已定提供 git ↔ server 双向 import/export。server 不碰明文，转换只能在 CLI 侧做。本子 change 落地迁移子命令，让用户在两个 provider 之间自由搬迁，也构成 server 模式的回滚通道。

## What Changes

- 新增 `senv migrate to-server` / `senv migrate from-server` 双向迁移命令
- 迁移以密文条目为单位逐条搬运（含 metadata blob），vault 口令不变化、不重新加密
- 目标端已有数据时显式确认或拒绝，不做静默合并

## Capabilities

### New Capabilities
- `server-migration`: git 仓与 senv-server 之间双向迁移的行为与安全约定

### Modified Capabilities
（无）

## Impact

- `cmd/`：新增 migrate 命令族
- 依赖 `senv-server-provider-client` 的 server provider 读写能力
