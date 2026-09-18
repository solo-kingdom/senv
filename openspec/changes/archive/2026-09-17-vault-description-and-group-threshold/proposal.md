## Why

vault 对人和 agent 都难导航：env/text 无说明，`set` 隐式建组把一级组撑爆；外部契约名靠 activate 切值，但同名覆盖静默，个人/工作会切错。

## What Changes

- 配置源（env/text/config 条目、Host、KeyPair、LLM Provider、MCP Server）与 env/text 分组增加**说明**；list 必带；存量空说明合法。
- **BREAKING**：env/text `set` 组不存在则失败；新建组必填非空说明。
- `env export` / `group activate` 对已激活组同名 key 打 warning，不阻断。

## Non-goals

嵌套组；Host/KeyPair 组一等公民化；LLM/MCP 分组；MCP 写 Host/LLM/MCP 说明；敏感标记/分 vault；存量强制回填；软件强制组名前缀。

## 安全性

说明随 vault 同步，list 可见、不含值。密钥不得写入说明（文档约束，不扫描内容）。硬上限 2KB，降低 list 载荷。MCP 的 SSH/LLM/MCP 工具保持只读。

## Capabilities

### New Capabilities

- `vault-description`: 说明字段的附着面、长度、list 可见性、可选/必填边界
- `group-threshold`: 禁隐式建组、新建 env/text 组必填说明、同名覆盖 warning

### Modified Capabilities

- `text-storage`: group add / set / list 对齐说明与闸门
- `ssh-assets`: Host/KeyPair 档案说明
- `llm-provider`: Provider 档案说明（不用模型目录文案）
- `llm-provider-mcp`: list 返回说明
- `mcp-server`: 说明长度上限与 list 对齐
- `config-grouped-storage`: 说明长度上限
- `config-tui`: 说明规则对齐
- `llm-provider-tui`: 表单可编辑 Provider 说明

## Impact

`internal/storage` 类型与 env/text group meta、`internal/{env,text,ssh,llm,config}`、`cmd/`、TUI 相关 Tab、MCP 工具入参/出参、`.agents/skills/senv-cli/SKILL.md`。同步载荷随密文 JSON 多一个可选字段，无需协议版本。
