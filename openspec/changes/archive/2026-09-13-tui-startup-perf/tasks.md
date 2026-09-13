## 1. A1: pull 网络移出锁外

- [x] 1.1 将 `internal/provider/server.go` 的 pull 拆为三段：读锁收集 manifest → 无锁 `api.Pull` → 写锁应用落盘；三段各加 perflog 埋点
- [x] 1.2 补并发测试：pull 网络期间本地写入不被阻塞、dirty 队列语义不变、应用阶段落盘原子
- [x] 1.3 `make check` 通过；用 `SENV_PERF_THRESHOLD=1 senv tui` + `~/.log/senv/perf.log` 对比拆锁前后 `tui.load-*` 与 `sync.autopull` 的重叠/耗时

## 2. A2: text 域单趟快照

- [x] 2.1 `internal/text` 新增 `Snapshot()`：一次 `withVaultRead` 内读取并解密全部条目，返回聚合结构（含分组信息）
- [x] 2.2 TUI text tab 改走 `internal/tui/snapshot.go` 进程内 memo（与 env 同机制），写操作/pull 应用后 `Invalidate()`
- [x] 2.3 验证测试：多条目 text vault 下锁获取次数不随条目数增长；内容与原有 `List` 路径一致

## 3. B: 真懒加载

- [x] 3.1 `internal/tui/model.go` Init 仅加载当前聚焦 tab + 共享快照；其余 tab 复用 History 的 `visited` 模式在首次聚焦时一次性加载
- [x] 3.2 全局搜索依赖未加载域时，先异步补齐该域快照再出结果（带加载态）
- [x] 3.3 修正 `model.go` 中「Tabs load their data lazily」注释与实现的一致性关系（注释兑现为行为）
- [x] 3.4 更新受影响的集成测试：统一改为「先切到目标 tab」前置；`make check` 通过

## 4. C: 加密快照缓存

- [x] 4.1 新增快照读写模块（建议 `internal/tui` 子包）：明文列表用 vault 主密钥 AES-256-GCM 加密落盘 `<dataPath>/tui-snapshot.enc`，权限 0700/0600
- [x] 4.2 实现指纹（salt + 密文清单聚合）与读取路径：解密失败/指纹不符/缺失时静默回退直接解密
- [x] 4.3 实现写入时机：TUI 正常退出 + 写操作/pull 应用后（best-effort，失败静默）；写操作后立即失效并同步重写
- [x] 4.4 启动流程接入：先渲染快照，后台真实解密完成后逐域比对，不一致即替换展示
- [x] 4.5 加功能开关 `SENV_TUI_SNAPSHOT=off` 可整体回退
- [x] 4.6 安全验证：快照文件内无明文（测试断言密文性质）；权限测试；损坏/篡改回退测试；`make check` 通过

## 5. 收尾

- [x] 5.1 用 perflog 验证 spec 预算：pull 网络缓慢时首屏仍出现、快照命中时首屏即时（贴数据到 change 备注或 docs/reviews）
- [x] 5.2 如 `cmd/tui.go` 帮助文本或 `.agents/skills/senv-cli/SKILL.md` 描述了启动行为，同步更新
- [x] 5.3 `openspec validate --change tui-startup-perf --strict` 通过
