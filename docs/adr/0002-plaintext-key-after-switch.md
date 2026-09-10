# 0002-plaintext-key-after-switch

LLM Provider 凭据存 vault（档案只存引用），但 `senv ai switch` 之后解密后的明文 key 会落在目标 agent 的配置文件中（文件权限收敛 0600；codex 例外，只写环境变量名）。作为安全工具写明文 key 是接受的妥协：切换的本意就是让 agent 直接可用，而 agent 自身无法从 vault 拉取凭据（MCP server 无法在 agent 的模型请求链路上注入认证）。事后若要收回明文，只能引入凭据代理转发，那会改变「senv 不做 LLM 请求代理」的边界（见 driver `agent-provider-switch` Non-goals），属有意取舍。
