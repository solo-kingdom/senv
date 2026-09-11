# 持久会话不使用操作系统钥匙串

远程登录无法操作 GUI 授权/解锁对话框，macOS 登录钥匙串作为持久会话存储会让每次读写都可能阻塞。决定：任何平台都不把操作系统钥匙串当作安全存储，也不提供 opt-in。Darwin 不再有 Apple 专用后端，与 Linux 共用 Unix 文件系统存储；无安全存储时的写目标见 [ADR-0016](./0016-unix-filesystem-session-store.md)。这修正了 [ADR-0009](./0009-session-expiry-model.md) 里「密钥在系统钥匙串里」的存储假设，到期/失效判定本身不变。

## Considered Options

- **修 ACL / 信任 `/usr/bin/security` 以求无弹窗**：SSH 下登录钥匙串锁定仍要本机 GUI 解锁，远程点不到。否决。
- **有 GUI 用钥匙串、检测到 SSH 再换后端**：探测不可靠，行为分裂，误入钥匙串仍会挂起。否决。
- **保留 `--keychain` opt-in**：双后端会让 SSH 路径再次踩弹窗。否决。
