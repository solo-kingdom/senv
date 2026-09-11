## 1. 组件核心

- [ ] 1.1 扩展 `internal/tui/list.go` 为共享列表组件：`List`（Move/Page/Home/End/VisibleRange）+ `paneBudgets` 双栏几何；验证：`go build` + 单元测试覆盖窗口边界（空列表/单行/游标越界/整页滚动）
- [ ] 1.2 收敛散落 helper：`modalBox`（env_tab）、`cursorLine`（ssh_tab）、`orDash`/`sortedKeys`（ssh_tab）、`clamp`/`maxLen`/`isPrintable`（env_tab）搬入共享文件，调用点改引用；验证：`go vet` 无未使用/重复定义

## 2. 试点迁移（零行为变化）

- [ ] 2.1 audit tab 迁移：删除手动 `top`/`pageSize`/`clampWindow`，改用组件 `Page`/`VisibleRange`；验证：同状态迁移前后渲染 diff 为空（游标顶/中/底 × PgUp/PgDn × `f` 过滤 3 组）
- [ ] 2.2 history tab 迁移：删除自算 `visibleRows`，改用组件（窗口由居中统一为跟随，内容集合不变）；验证：同状态 diff 仅窗口停留位置差异，时间线/详情/恢复流行为不变

## 3. 回归

- [ ] 3.1 `make check` + 8 Tab 切换冒烟（未迁移 Tab 不受影响），结果写入 proposal 验证记录；验证：退出码 0
