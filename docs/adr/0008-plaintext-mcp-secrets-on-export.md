# 0008-plaintext-mcp-secrets-on-export

MCP Server 档案的 `env` / `headers` 值（往往就是 token）随档案加密存 vault，但 `senv mcp export` 会把解析后的明文写进目标 agent 的全局配置文件（权限收敛 0600）。这是 ADR-0002 的同一取舍换到 MCP 面：agent 无法从 vault 拉取凭据，agent 配置是它唯一的读取路径，而导出这个特性的全部意义就是让 server 直接可用。允许值写 `{{env:<group>:<key>}}` 引用只解决「同一份值不在 vault 里重复存」（引用按 ref-system 在导出时解析，见既有语义），不改变导出等于明文落盘这一事实；要真正收回明文，只能引入凭据代理转发，那会扩张 senv 的边界。

## Consequences

- 导出计划必须显式标注哪些条目会把明文写进哪个文件，并在写入前确认——落盘位置与内容属用户可见的安全信息。
- agent 配置文件按 0600 写入、改动前备份；这保护不了同机同用户的其它进程，安全性依赖用户本机的账户边界。
