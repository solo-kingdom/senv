# 0005-tui-edit-coverage

TUI 的定位从「以浏览为主、写路径只覆盖高频项」改为「尽可能包含编辑能力」：CLI 能做的数据编辑，TUI 都要能做，包括 SSH host/keypair 与 LLM provider 的完整 CRUD。TUI 是唯一能同时看到「vault 数据」与「本机实况（agent 指向、落盘路径）」的界面，只读 Tab 会把用户推回命令行再切回来，打断操作流；`internal/tui/ssh_tab.go` 与 `internal/tui/ai_tab.go` 里自述的 `read-only` 是要拆除的现状，不是目标。

边界：`session` 管理、`rekey`、`mcp install`、自更新、server 端管理（user/client/block）、`passwd` 明确留在 CLI——它们改本机信任根或需要更长的确认语义。SSH `keypair materialize`（明文私钥落盘）可以进 TUI，但必须显式二次确认并展示落盘路径。

凭据处理沿用 ADR-0002 的取舍：切换后明文写入 agent 配置是接受的妥协；TUI 新增的凭据录入必须遮蔽，且任何渲染文本不得出现凭据明文。
