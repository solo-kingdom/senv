# 持久会话用 Unix 文件系统，不按操作系统分后端

所有平台共用一套存储选型：能证明是内存文件系统则写入安全存储；否则写入磁盘逃生舱。不自动创建 RAM Disk，不引入凭据代理。Linux 通常有 `XDG_RUNTIME_DIR` tmpfs，未证明时仍须显式逃生舱（fail closed）。stock Darwin 无法证明 memory-backed，磁盘逃生舱是默认写目标，`session start` 写入时警告；用户自备 tmpfs 并让探测通过则走安全存储。升级路径不再调用钥匙串，遗留条目不读不删。到期/失效仍按 [ADR-0009](./0009-session-expiry-model.md)：duration 只看 `expires_at`（落盘则跨重启仍有效），restart 才看 boot ID；Darwin 不另写判定。这修正 ADR-0009「不把逃生舱常规化」在无法提供安全存储的平台上的结论。

## Considered Options

- **无安全存储一律 fail closed（含 Darwin）**：与 Linux 字面相同，但 stock Mac 远程主路径必须每次 `--insecure-cache`。否决。
- **自动创建 RAM Disk / 凭据代理**：能避免派生钥落盘，但 Darwin 特化、残留设备与新后端成本高。否决。
- **配置项一次 opt-in 后才落盘**：比每次 flag 好，仍要先改配置才能远程用。否决。
- **Darwin 默认落盘时 duration 也按 boot ID 作废 / 单独缩短 max_lifetime**：重启体感接近 tmpfs，但到期模型按平台分裂。否决。
