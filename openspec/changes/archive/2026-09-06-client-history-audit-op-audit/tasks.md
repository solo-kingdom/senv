## 1. 事件模型与 helper（安全：高优先级）

- [x] 1.1 扩展 internal/session/audit.go：新增业务事件类型与统一 {time,type,target,outcome,detail} 结构（不含值）；配对单测：各事件类型序列化与断言不含敏感值字段（验证：go test 通过）
- [x] 1.2 best-effort 写入 helper：写失败仅一次 stderr 告警、不改退出码；权限维持 0600/0700；配对单测：文件不可写时业务不受影响（验证：go test 通过）

## 2. 命令埋点

- [x] 2.1 env/text/config 增删改命令埋点（含 shorthand 地址形式）；配对单测：成功与失败各产生一条事件且目标正确（验证：go test 通过）
- [x] 2.2 config install/uninstall、sync（push/pull，含 autosync 阻塞推送）与冲突解决埋点；配对单测：autosync 节流窗口内不重复刷事件（验证：go test 通过）

## 3. 查看入口

- [x] 3.1 新增 `senv audit` 命令：新到旧列表、--since/--until/--type/--limit 过滤、坏行跳过提示；配对单测：日期边界过滤与坏行容错（验证：go test 通过）
- [x] 3.2 TUI 新增审计 Tab：时间线浏览（含日期时间）、类型过滤、翻页、空态提示；验证：手工浏览核对渲染与外框不溢出（参考既有 frame 测试）

## 4. 回归与闭环验证

- [x] 4.1 `make check` 全绿；session/MCP 既有审计事件回归通过（验证：记录命令输出）
- [x] 4.2 手工闭环：执行 env set/text set/config install/sync/冲突解决 → `senv audit` 按日期查看 → TUI 审计 Tab 浏览；验证：结果写入 proposal 验证记录
