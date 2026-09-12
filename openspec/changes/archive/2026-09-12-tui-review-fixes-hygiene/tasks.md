# tasks: tui-review-fixes-hygiene

## 1. search.go 窗口化卫生

- [x] 1.1 `Update` 处理 `tea.WindowSizeMsg`（search.go:194）＋ 用例（resize 后窗口尺寸正确）
- [x] 1.2 头部截断到 innerW（search.go:289）＋ 用例（长输入/长 range 不溢出）
- [x] 1.3 行先截断后样式，选中行改用共享 `cursorLine`（search.go:278/281）＋ 用例（选中行无转义宽度误计）
- [x] 1.4 删除不可达 `{[]string{"type"}}` 键位声明（search.go:62）

## 2. list / helpers 内部缺陷

- [x] 2.1 `paneBudgets` 总宽收口（list.go:207）＋ 用例（小宽度下 left+right+5 ≤ width）
- [x] 2.2 `Page` 退化语义与 godoc 对齐（list.go:193）＋ `SelectVisible` 空集显式 no-op（list.go:239）＋ 用例
- [x] 2.3 包级 `max` 改名 `maxInt` 消除内置遮蔽（helpers.go:49）＋ 删除 no-op `maxLen`（helpers.go:46）

## 3. 散点

- [x] 3.1 config 新建表单 group 字段补 validate（config_tab.go:727）＋ 用例（非法分组名被拒）
- [x] 3.2 ai_tab 重复条件修复（ai_tab.go:1039）：删除重复子表达式（行为无差异，编译期证明，无独立用例）
- [x] 3.3 env 死代码清理：`envWithGroup`（env_tab.go:213）、`preview :=`（env_tab.go:223）、`_ = i`（env_tab.go:1130）

## 4. 收尾

- [x] 4.1 对照审查报告核对 hygiene 销账清单无遗漏；`make check` 全绿
- [x] 4.2 `openspec validate --strict --type change tui-review-fixes-hygiene` 通过
