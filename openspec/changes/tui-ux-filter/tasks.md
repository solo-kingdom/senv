## 1. 共享过滤组件

- [ ] 1.1 新建 `internal/tui/filter.go`：`Filter` 输入状态机（append/backspace/clear）+ 渲染提示行 + `Match` 谓词挂接（统一走 `matchKey`）；验证：单元测试覆盖空词/追加/回删/esc/大小写
- [ ] 1.2 env/text/config/audit 过滤迁移到共享实现（匹配、游标重置、esc 语义保持现状；audit `f` 预设作为叠加谓词保留）；验证：四 Tab 过滤行为与迁移前一致（同词同结果集）

## 2. 补齐 SSH/AI/MCP

- [ ] 2.1 ssh_tab 接入：`/` 过滤左栏主列表（alias/hostname），右栏联动；验证：输入 `web` 仅剩匹配 host，光标可用 `↑↓` 在匹配集内移动
- [ ] 2.2 ai_tab 接入：过滤 provider（alias/base_url 标识部分）；验证：同上
- [ ] 2.3 mcp_tab 接入：过滤 server 档案（alias/command）；验证：同上

## 3. 文档与回归

- [ ] 3.1 同步 `.agents/skills/senv-cli/SKILL.md`（SSH/AI/MCP 支持 `/` 过滤）；验证：文档与实现一致
- [ ] 3.2 `make check` + 全 Tab `/` 冒烟 + 数据未就绪（懒加载中）时过滤不 panic，结果写入 proposal 验证记录；验证：退出码 0
