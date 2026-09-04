## 1. 状态结构与护栏

- [x] 1.1 【高优先级】扩展 `syncState` 结构：新增 `vault_binding`（server 指纹 + vault 名）与 `written_by`（path/pid/ts）字段；`NewServerProvider` 计算地址指纹存入 provider，测试构造函数支持注入合成绑定。验证：单测覆盖指纹计算与 JSON 序列化字段名。
- [x] 1.2 【高优先级】实现 `writeStateChecked`：对照磁盘现有状态拒绝"entries 净减且无对应 tombstone"与"metadata_hash 非空→空"；`saveState` 与 `applyRemote` 收敛到该咽喉；bootstrap/accept-remote/migrate 走显式重建选项（acceptRemote 仅在 GetMetadata 404 时允许空哈希）。验证：单测覆盖三种拒绝路径与两类合法重建路径。
- [x] 1.3 【高优先级】`loadState` 校验 vault 绑定：不符时报错并指引 `--accept-remote`；旧文件（无字段）正常加载，首次成功写入补全绑定与来源。验证：单测覆盖不符报错、旧文件兼容、写入补全三个场景。

## 2. 假冲突自愈

- [x] 2.1 【高优先级】`pullLocked` metadata 自愈：`st.MetadataHash != localHash` 且 `localHash == remoteHash` 时收养哈希，不写 metadata 文件、不算冲突。验证：单测用两端一致 + 快照哈希为空的状态，断言 sync 成功且哈希被收养。
- [x] 2.2 【高优先级】`pushLocked` 409 分类自愈：对 `BaseRevision==0` 的冲突条目经 `Pull(0)` 按 identity 过滤比对密文哈希，一致则收养远端 revision 并剔除出待推送清单、重试剩余推送；其余维持 `SyncConflictError`。`PushResult` 增加 `Healed` 计数。验证：单测覆盖快照缺失自愈、删除冲突不自愈、哈希不同仍报冲突、Pull(0) 网络失败不落盘不误报。
- [x] 2.3 `pushLocked` metadata 冲突判定前置收养：`remoteHash == localHash` 时先收养再判冲突。验证：单测覆盖 metadata 假冲突消除且真实冲突仍报错。

## 3. 输出与端到端验证

- [x] 3.1 `senv sync` 输出自愈提示：发生收养时低噪声报告"已自动修复 N 条同步状态"（Pull/PushResult 汇总到 SyncWithReport）。验证：命令层测试断言输出与退出码；无自愈时输出不变。
- [x] 3.2 【高优先级】端到端回归：用故障报告序列构造状态（360 条含 config_index → 手工删除 config_index 快照 + 清空 metadata_hash 模拟损坏），执行 sync，断言自动收养、无冲突、数据字节不变。验证：`go test -race ./internal/provider/` 全绿。
- [x] 3.3 【高优先级】护栏端到端回归：并发 AutoPush / 穿插前台 sync 的复现序列下，状态文件不得出现退化形态。验证：扩展 `TestAutoSyncConcurrentWritesKeepStateConsistent` 覆盖 config_index 与 metadata。
- [x] 3.4 `make check`（fmt + vet + lint + test -race）全绿；确认无新 CLI 参数、无 server API 变更。
