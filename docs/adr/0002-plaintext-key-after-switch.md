# 0002-plaintext-key-after-switch

LLM Provider 凭据存 vault（档案只存引用），但 `senv ai switch` 之后解密后的明文 key 会落在目标 agent 的配置文件中（文件权限收敛 0600；codex 例外，只写环境变量名）。作为安全工具写明文 key 是接受的妥协：切换的本意就是让 agent 直接可用，而 agent 自身无法从 vault 拉取凭据（MCP server 无法在 agent 的模型请求链路上注入认证）。事后若要收回明文，只能引入凭据代理转发，那会改变「senv 不做 LLM 请求代理」的边界（见 driver `agent-provider-switch` Non-goals），属有意取舍。

（补充 2026-10-20，ADR-0031）：「一律写明文」收窄为「仅对不支持环境变量反解的 agent 写明文」。调研发现 codex/kimi/pi/omp/opencode 的配置都支持环境变量引用，明文不再是唯一选择；`senv ai switch` 默认 `--credential-mode=auto` 对这些 agent 只写引用，明文由 `senv env export` 注入 shell。claude-code 的 settings.json 不展开 `${VAR}`，auto 回落 inline；凭据代理仍属 Non-goal。
