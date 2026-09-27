## 1. agentcfg：omp 目标与路径解析

- [x] 1.1 `internal/agentcfg/agents.go` 新增 `OmpAgentDir(home)`：镜像 omp 规则（OMP_PROFILE>PI_PROFILE，非法回退默认、PI_CONFIG_DIR、PI_CODING_AGENT_DIR 仅默认 profile），附源码锚点注释；XDG 不支持写明
- [x] 1.2 `Target` 加 `Aliases []string`；`Find`/`IDs` 保持展示面只用 `ID`，`Find` 匹配别名（大小写不敏感）
- [x] 1.3 `Supported()` 新增 `omp` 目标：FormatJSON、`mcpServers` 键、user `OmpAgentDir/mcp.json`、project `.omp/mcp.json`、Remote{HTTP,SSE,Headers,TypeKey}+Reason、无 Prerequisite、Note 提示 `/mcp reload`
- [x] 1.4 `internal/agentcfg/config_test.go`：路径解析全分支（默认/profile/非法 profile/PI_CONFIG_DIR/PI_CODING_AGENT_DIR 有无 profile）、别名查找、project scope 相对路径

## 2. llm：YAML 写回原语与 omp 适配器

- [x] 2.1 `internal/llm/switch.go` 新增 `applyYAMLMerge`（yaml.v3 解码 `map[string]any` → mutate → 编码写回），纳入 `configTransaction` 事务/备份/原子替换协议；`go.mod` 中 yaml.v3 转直接依赖
- [x] 2.2 抽出 pi 适配器的 provider/模型条目构造为共享函数（pi 与 omp 共用，`compat.supportsDeveloperRole:false` 保留并注释指向 ADR-0030/pi 坑）
- [x] 2.3 `AgentAdapter` 加 `Aliases`，`LookupAgent` 匹配别名
- [x] 2.4 新增 `ompAdapter`：`models.yml` 写 `providers.senv-<alias>`（piAPIType 复用、清理 prior provider 条目）+ `config.yml` 写 `modelRoles.default`（其它键/角色保留）；`ConfigPaths` 返回两个 YAML 路径；CredentialInline
- [x] 2.5 `switch_test.go`：omp 切换全场景（spec「omp 写回契约」「omp 配置路径解析」「omp 兼容字段投影」「agent id 别名查找」的 scenario 逐条对应）；YAML merge 保留无关键、事务回滚

## 3. 端到端与文档

- [x] 3.1 `cmd/mcp_install_test.go`、`cmd/mcp_export_test.go`：omp 出现在安装/导出清单，install omp 无前置依赖步骤、写 `~/.omp/agent/mcp.json`，别名 `oh-my-pi` 可用
- [x] 3.2 `go run . --help`、`go run . mcp install --help`、`go run . ai switch --help`、`go run . mcp list-tools` 输出检查（omp 出现、别名不出现）
- [x] 3.3 `.agents/skills/senv-cli/SKILL.md`：agent 清单加 omp；pi 条目旁补 omp 差异（内置 MCP、models.yml/config.yml、modelRoles.default）；记录 omp 验证锚点（v18.3.3 + 上游文档/源码路径）
- [x] 3.4 临时 HOME 冒烟（`HOME=/tmp/omp-smoke`）：测试 vault 切换 omp + `mcp install omp`，验证两个 YAML 与 mcp.json 落盘形状；用同 HOME 启动 omp 非交互模式确认配置被解析加载（schema 错在启动即报 vs 进入网络调用即为通过）；验证产物与命令记录留 `/tmp/omp-smoke/`
- [x] 3.5 `make check`（fmt+vet+lint+test -race）全绿
- [ ] 3.6 commit（跟随仓库 log 风格，说明写原因）；ADR-0030 状态 proposed→accepted 随实现更新；OpenSpec archive 走单独流程
