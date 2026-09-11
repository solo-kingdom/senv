## 1. 侧栏组件下沉

- [x] 1.1 config 侧栏渲染（All 伪组/计数/焦点态/双栏几何）提取进列表组件，config tab 改为消费共享实现；验证：config tab 渲染 diff 为空（同状态前后对比），`openspec validate --strict --type change tui-ux-sidebar` 之外先过 `make test`

## 2. env/text 迁移

- [x] 2.1 env tab 升级侧栏：All 置顶默认选中、default 置顶 + `(default)`、`●` 标记、计数随过滤更新、All 视图 `group/key` 前缀、`←→/hl` 焦点切换定位第一条；验证：默认全览/计数/聚焦三场景手工核对 + spec 场景逐条过
- [x] 2.2 text tab 升级侧栏：同上，空分组显示计数 0，选中空组右侧显示空状态提示；验证：建空分组后侧栏可见、All 视图无该组条目
- [x] 2.3 全局搜索跳转适配：跳转 env/text 条目时侧栏选中所属真实分组并定位条目；验证：`S` 搜索跳转后左栏非 All 且光标落条目
- [x] 2.4 `t`/重命名/删除等组操作在侧栏语境回归；验证：default 保护、激活组删除提示均不变

## 3. 文档与回归

- [ ] 3.1 同步 `.agents/skills/senv-cli/SKILL.md`（env/text 侧栏描述）；验证：文档与实现一致
- [ ] 3.2 `make check` + env/text/config 三 Tab 过滤计数一致性冒烟，结果写入 proposal 验证记录；验证：退出码 0
