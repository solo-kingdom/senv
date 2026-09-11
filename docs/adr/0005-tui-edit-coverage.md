# 0005-tui-edit-coverage

TUI 的定位从「以浏览为主、写路径只覆盖高频项」改为「尽可能包含编辑能力」：CLI 能做的数据编辑，TUI 都要能做，包括 SSH host/keypair、LLM provider 与 MCP Server 档案的完整 CRUD，以及 MCP 导出/撤回（TUI 能同时看到档案与各 agent 的导出状态）。只读 Tab 会把用户推回命令行再切回来，打断操作流。

边界：`session` 管理、`rekey`、`mcp install`、自更新、server 端管理（user/client/block）、`passwd` 明确留在 CLI——它们改本机信任根或需要更长的确认语义。MCP 的 `--print`、`--scope project` 与非 stdio 传输也留在 CLI。SSH `keypair materialize`（明文私钥落盘）可以进 TUI，但必须显式二次确认并展示落盘路径。

凭据处理沿用 ADR-0002 的取舍：切换或 MCP 导出后明文写入 agent 配置是接受的妥协；TUI 新增的凭据录入必须遮蔽，且任何渲染文本不得出现凭据明文。MCP 导出计划只标注「明文 env」与目标路径，不渲染解析后的值。MCP 档案的 env 字面量在列表/详情/计划中不出现；用户在表单里显式打开 `$EDITOR` 编辑 env 时，编辑器是 TUI 内唯一解密面（对标 Text/Config 的 `e`，不对标 AI 凭据字段）。
