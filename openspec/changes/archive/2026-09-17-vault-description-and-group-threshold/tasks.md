## 1. 存储与校验

- [x] 1.1 增加 `MaxDescriptionBytes` 与 `ValidateDescription`（trim、上限；组新建另要求非空）
- [x] 1.2 `EnvVarEntry`/`EnvGroupMeta`/`TextEntry`/`HostEntry`/`KeyPairEntry`/`LLMProviderEntry` 增加 `Description`；config/MCP 写入走同一校验
- [x] 1.3 text 组 `.meta.enc` 读写（缺失=空说明）；env 组 meta 读写带上 description，rename 时保留

## 2. env/text 闸门

- [x] 2.1 `env.Set` 及所有写入：组不存在则失败，不再 `NewEnvGroup`
- [x] 2.2 `text.Set`/`SetFromFile`/`import`：组不存在则失败
- [x] 2.3 `AddGroup(name, description)` 必填非空说明；CLI `env/text group add --description`；MCP `senv_group_add` 必填
- [x] 2.4 更新既有 AddGroup 测试与安全边界测试

## 3. 条目/档案说明读写

- [x] 3.1 env/text set/get/list 支持 `--description`；更新时未传 flag 则保留原说明；MCP env/text set/list 对齐
- [x] 3.2 host add/edit/get/list 与 keypair import/edit/list；MCP ssh_host_list/get 返回说明
- [x] 3.3 `ai provider add/edit/list/show` 与 MCP `llm_provider_list`
- [x] 3.4 MCP Server 与 config 说明超限拒绝；list 已有说明则保持

## 4. 同名覆盖 warning

- [x] 4.1 共享检测：激活组同名 key → warning 列表（key、各组、覆盖者）
- [x] 4.2 `env export` stderr；`group activate` 成功后 warning；MCP `senv_env_export` 带上 warnings
- [x] 4.3 测试：冲突有 warning、无冲突无 warning、仍 exit 0

## 5. TUI

- [x] 5.1 env/text：新建组必填说明；条目表单可选说明；组侧栏/详情可见说明
- [x] 5.2 SSH Host/KeyPair 表单与详情
- [x] 5.3 AI Tab 表单与详情
- [x] 5.4 config 说明超长表单内失败

## 6. 文档与回归

- [x] 6.1 更新 `.agents/skills/senv-cli/SKILL.md`：闸门已生效、说明 flag、MCP 字段、同名 warning
- [x] 6.2 `go test` 覆盖改动包；`go run . --help` 与相关 `--help`、`mcp list-tools` 抽查
