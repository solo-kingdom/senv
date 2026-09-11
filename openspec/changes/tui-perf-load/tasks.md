## 1. storage 读路径

- [x] 1.1 批量读 API：单次锁内「清算 + manifest 读 + 批量解密」的 vault 快照装载；既有逐文件 API 保留
- [x] 1.2 manifest 进程内缓存：以 metadata.json 代际（mtime+size）为失效界，本进程写/恢复后主动失效；锁语义与混合代际隔离不变
- [x] 1.3 ADR 落盘：`docs/adr/` 新增「读路径锁语义」ADR（吸收 grill 候选 adr-读路径锁语义）
- [x] 1.4 单元测试：批量读与逐文件读结果一致、写后失效、代际变化重载、rekey 事务后读取安全、`-race` 并发读

## 2. TUI 快照与 reload

- [ ] 2.1 TUI 快照 registry：单趟生成、原子替换、写消息失效、single-flight 重建；env Tab 去除 ListGroups+List 双趟，搜索/deref/AI Tab 凭据引用收集改读快照（env Tab 单趟已交付；共享内存 registry 待用户确认是否简化——批量装载实测 13ms 后其额外收益趋近于零）
- [x] 2.2 Reload 两阶段 stale-while-revalidate：不清空展示、后台重建后静默替换，保留光标/过滤/表单状态
- [x] 2.3 装载完成消息广播到所有 Tab（修后台 Tab 装载被丢弃、切回重载的问题）

## 3. 同步状态增量

- [ ] 3.1 `LocalSyncSnapshot`/collect 增量收集：无写入变更时 stat 级比对零解密，写入后仅重读变更条目；dirty 判定与全量等价
- [ ] 3.2 单元测试：增量与全量收集等价、无变更零解密、写入后增量正确

## 4. 回测与收尾

- [x] 4.1 以耗时日志回测：单趟全量装载 ≤300ms；TUI 暖启动列表可用 ≤1.5s；SWR 场景（pull 应用变更）列表不清空人工核对
- [ ] 4.2 `openspec validate --strict --type change tui-perf-load` 通过，全量 `make check` 通过
