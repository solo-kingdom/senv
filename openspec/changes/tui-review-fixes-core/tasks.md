# tasks: tui-review-fixes-core

每条任务对应审查报告销账编号（文件:行）；完成即勾，行为以回归用例锁定。

## 1. All 伪组语义（选择键 / 复制 / 解引用 / 渲染前缀）

- [x] 1.1 env `a` 全选键改真实分组标识（env_tab.go:390）＋ All 视图行前缀条件（env_tab.go:1144）＋ 用例（All 视图 `a` 后 `d` 批量目标正确、前缀显示 group/key）
- [x] 1.2 text `a`/`visibleKeys` 改真实分组标识（text_tab.go:345/1082）＋ All 视图前缀（text_tab.go:1096）＋ 用例
- [x] 1.3 text `y` 复制改条目真实分组（text_tab.go:960）＋ 用例（All 视图复制成功）
- [x] 1.4 env `resolveDeref` All 视图取真实分组（env_tab.go:220）＋ 用例

## 2. 过滤感知游标（ai / mcp / ssh）

- [x] 2.1 mcp `statusFor`/`clamp` 切可见列表（mcp_tab.go:206/562）＋ 用例（过滤态下导出计划定位正确）
- [x] 2.2 ssh `focusListLen` 过滤长度 + `applyPendingJump` 清过滤契约（ssh_tab.go:426/1264/1271）＋ 用例
- [x] 2.3 ai 左栏可见集游标全面核对（ai_tab.go:586：clampLeft/currentProvider/providerLines/jumpFocus/focusListLen）＋ 用例
- [x] 2.4 ai 过滤输入态吞导航/实体键（ai_tab.go:353）＋ 用例（输入 `/web` 期间按 j/k/g/G/n/e/d 不改游标不触发动作）

## 3. 多选与审计缺口（config / mcp / text / env）

- [x] 3.1 config space 兼容 `" "`（config_tab.go:481）＋ 用例（空格勾选生效）
- [x] 3.2 config `enterSelectionPlan` nil guard（config_tab.go:856）＋ `selectedNames` 全量选择集语义（config_tab.go:892）＋ 选择集 reconcile（config_tab.go:1011）＋ 用例
- [x] 3.3 mcp/text 单选交错 `e`/`d` 提示不误操作（mcp_tab.go:395/422、text_tab.go:353）＋ 用例
- [x] 3.4 mcp `replanForce` 多选计划（mcp_tab.go:934）＋ 审计目标真实别名（mcp_tab.go:990/1022）＋ `ClearSelection` 统一（mcp_tab.go:340）＋ 用例
- [x] 3.5 text `doBatchDelete` 逐条审计 + reload（text_tab.go:636/646）＋ 批量导出路径复验（text_tab.go:653）＋ 用例
- [x] 3.6 env 幽灵勾选防护（env_tab.go:385）＋ `doActivate` ok 独立判断（env_tab.go:426）＋ 用例

## 4. 收尾

- [x] 4.1 对照审查报告核对 core 销账清单无遗漏；`make check` 全绿
- [x] 4.2 `openspec validate --strict --type change tui-review-fixes-core` 通过
