## 1. TUI Backup Tab

- [x] 1.1 新增 `internal/tui` Backup Tab：双栏 All + 分组，列表只显示 key/size/时间/说明；键位对齐 Text（无 `D`）；`default` 不可改名/删除；`+` 必填说明。验证：TUI 相关测试或可脚本化的 model 测试覆盖列表不含 value
- [x] 1.2 全局搜索 `S` 纳入 backup 的 group/key/description，不搜 value，结果可跳转 Backup Tab。验证：搜索测试含 backup 命中与 value 不命中
- [x] 1.3 启动快照/按需加载覆盖 Backup Tab，不破坏现有 Tab 懒加载。验证：既有 tui 启动测试不回退

## 2. MCP（高优先级·安全）

- [x] 2.1 注册 `senv_backup_get/set/delete/list`；list 无 value；set 省略 description 则保留；无 decode。验证：`go run . mcp list-tools` 含四工具；MCP 单测覆盖 list 不含正文、缺组失败
- [x] 2.2 `senv_group_add`/`senv_group_list` 支持 `kind=backup`，description 必填。验证：MCP 分组测试

## 3. 文档与收尾

- [x] 3.1 更新 skill 的 TUI 键位与 MCP 工具清单（含 21→25 或实际计数）。验证：与 `mcp list-tools` 一致
- [x] 3.2 `make check` 通过。验证：`make check`
