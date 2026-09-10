## 1. 注入与注册

- [x] 1.1 `tui.Managers` 扩展 LLM/LLMPointer/LLMHome 字段；model.go 按惯例注册 AI Tab（nil 跳过）
- [x] 1.2 `cmd/tui.go` 解锁后构造 ProviderManager 并注入

## 2. 浏览与切换

- [x] 2.1 aiTab 浏览视图：左栏 provider 列表、右栏详情（base_url/模型集/凭据引用）+ 指针区（已切换/未切换/不支持），空态提示
- [x] 2.2 切换状态机：`s` → agent 选择 → 模型选择 → 确认；异步 Switch tea.Cmd
- [x] 2.3 结果处理：成功刷新指针 + 提示（codex 附加 env 名）；失败错误横幅且指针不变

## 3. 测试与收尾

- [x] 3.1 Tab 测试：注册/跳过、浏览渲染无凭据明文、切换成功刷新、失败横幅、codex 提示
- [x] 3.2 README「TUI mode」键位表补充 AI Tab
- [x] 3.3 `make check` 全绿；`openspec validate agent-provider-switch-tui --strict` 通过；回填 proposal 验证记录
