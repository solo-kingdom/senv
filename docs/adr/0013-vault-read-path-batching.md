# 0013-vault-read-path-batching

vault 读路径从「每个文件一次排他 flock + rekey 清算（重读 manifest）+ config/data root 开闭」改为两个手段：**批量装载**（`LoadEnvVaultWithKey` 单次锁、单个 data root 读完全 vault）与 **rekey manifest 进程内缓存**（以 metadata.json 的 stat 代际 + journal 文件存在性为失效界，读路径不再逐次 OpenRoot+Read+校验 manifest）。实测 519 文件规模的全量装载从 ~1.2s 降到预期 300ms 以内（grill 基线：`env.list-groups` 1185ms、CPU 占比 2%，开销全部是锁与文件系统往返）。

代价与边界：正确性论证从「每次都查」变为「失效界证明」——未完成 rekey 的 journal 在持锁清算时必被 stat 捕获（rekey 事务经 securefs root 写入 journal，存在性维度必变化），且所有装载仍过排他锁，混合密钥代际隔离不削弱；但这条推理链是后人费解点，改 rekey 事务或 securefs 可见性规则时必须重新审视本 ADR。批量 API 已成为 TUI 数据面（快照装载）的依赖，回退到逐文件语义需同步改消费方。
