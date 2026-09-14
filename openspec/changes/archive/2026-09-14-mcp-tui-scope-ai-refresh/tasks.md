# Tasks: mcp-tui-scope-ai-refresh

## 1. MCP Tab scope 切换（design D1）

- [x] 1.1 `mcpTab` 增加 `scope` 字段（默认 `user`）与 `s` 切换键：`exporter()` 改读 `t.scope`；切换后 `syncStatus()` 重算右栏；右栏标题拼接当前 scope（`Agents · export status (scope: project)`）；Bindings/keyactions 注册 `s`（验证：临时 HOME 起 TUI 用例，按 `s` 后标题含 `scope: project` 且状态列重算，再按回到 `user`；Bindings() 含 `s` 条目）
- [x] 1.2 1.1 配对单测：scope 切换贯穿 `statusFor`/`startExport`/`startUnexport`/`replanForce` 四个消费方，未切换时与现状字节级一致（验证：`go test ./internal/tui/ -race -run 'MCP.*Scope' 全绿`）

## 2. MCP 导出与撤回按 scope 执行（design D2）

- [x] 2.1 scope=project 时 `x/X` 导出计划 cursor 目标为 `.cursor/mcp.json`、其余 agent 路径不变，`u/U` 撤回计划同 scope；确认执行后写盘与台账行为不变（验证：用例断言 export/unexport plan 的 `Path`；临时目录下执行导出后 `.cursor/mcp.json` 存在、台账记录指纹，user 级全局文件未被创建）
- [x] 2.2 2.1 配对单测：导出与撤回两路 × user/project 两 scope 矩阵（验证：`go test ./internal/tui/ -race -run 'MCP' 全绿`）

## 3. AI Tab 目录刷新（design D4/D5）

- [x] 3.1 `aiTab` 注册 `R` 键：异步 Cmd 调 `llm.Fetch(llm.DefaultCatalogURL, nil)` → `Counts()` 校验 → `llm.Save(t.mgr.LLMCatalog, cat)`；成功 okToast（含 provider/model 计数）+ `t.load()`；失败红色 toast 且旧缓存不动；`LLMCatalog == ""` 时 warn toast 指路 CLI；Bindings 注册 `R`（验证：`httptest.Server` 假目录源用例按 `R` 断言 toast 含计数且缓存文件更新；500/非法 JSON 用例断言红色 toast 且缓存文件字节不变；空路径用例断言 warn toast 零写盘）
- [x] 3.2 3.1 配对单测：含 `ctrl+r` 回归——按 `ctrl+r` 后假目录源零请求、仅本地重载（验证：`go test ./internal/tui/ -race -run 'AI.*Catalog|AI.*Refresh' 全绿`）

## 4. 文档与回归

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md`（MCP Tab `s` = scope 切换、AI Tab `R` = 刷新目录缓存）并订正 `ai_tab.go` 空态提示残留文案 "then r to refresh"（改为指向 `R`）（验证：所述按键与 Bindings 输出一致抽查）
- [x] 4.2 全量回归（验证：`make check` 全绿）
