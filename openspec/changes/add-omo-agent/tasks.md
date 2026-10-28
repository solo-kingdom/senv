## 1. agentcfg：omo 目标与路径解析

- [x] 1.1 `internal/agentcfg/agents.go` 新增 `OmoAgentDir(home)`：`OMO_CODING_AGENT_DIR` → `SENPI_CODING_AGENT_DIR` → `PI_CODING_AGENT_DIR` → `~/.omo/agent`；附二进制锚点注释（v5.1.47）并写明不跟随项目级目录搜索
- [x] 1.2 `Supported()` 新增 `omo` 目标：FormatJSON、`mcpServers` 键、user `OmoAgentDir/mcp.json`、project `.omo/mcp.json`、`Aliases: ["oh-my-openagent"]`、Remote{HTTP, Headers, TypeKey}（SSE 为 false + Reason）、无 Prerequisite、Note 提示 `/mcp` 重新加载
- [x] 1.3 `internal/agentcfg/config_test.go`：`OmoAgentDir` 三个环境变量优先级与 home 默认、user/project 路径、sse 的 `RemoteError` 报错与 http/stdio 放行、别名查找命中且 `IDs()` 不含别名

## 2. llm：omo 适配器与共享写回

- [x] 2.1 抽取 pi 的 `Apply` 写回体为 `applyPiFamily(req, settingsPath)`（provider 条目 + defaultProvider/defaultModel + `updatePiEnabledModels`），pi 行为不变
- [x] 2.2 新增 `omoAdapter`：ID `omo`、别名 `oh-my-openagent`、`OmoAgentDir` 下的 models.json + settings.json、`CredentialEnvRef` 返回 `"$"+name`、Protocol OpenAI 兼容、复用 `piAPIType` 与共享写回
- [x] 2.3 `internal/llm/switch_test.go`（或新文件）：omo 切换全场景——写 provider 条目与 settings 两键、`enabledModels` 置顶并清理 prior scope、`api_shape` 投影、`compat.supportsDeveloperRole:false`、凭据 env/inline 两模式、事务回滚；pi 既有测试全绿证明抽取无回归

## 3. 端到端与文档

- [x] 3.1 `cmd/mcp_export_test.go` / `cmd/mcp_install_test.go`：omo 出现在清单，install omo 无前置依赖步骤、写 `~/.omo/agent/mcp.json`，别名 `oh-my-openagent` 可用，sse 档案 plan-error
- [x] 3.2 检查 `go run . --help`、`mcp install --help`、`ai switch --help`、`mcp list-tools`：`omo` 出现、别名不出现
- [x] 3.3 `docs/adr/0032-omo-agent-json-writeback.md` 新增（接入范围、路径取舍、sse 不支持、skill 面已覆盖）
- [x] 3.4 `.agents/skills/senv-cli/SKILL.md` 与 `README.md`：agent 清单加 omo，写明 skill 面由 `.agents/skills` 覆盖、验证锚点 v5.1.47
- [x] 3.5 隔离目录冒烟：`OMO_CODING_AGENT_DIR=/tmp/senv-omo-smoke/agent` 下由真实适配器写出 models.json/settings.json，`omo --list-models` 列出 `senv-main` 且 m1 的 300K/32K/thinking 与投影一致；mcp.json 写入 stdio+http 后被 OmO 正常加载（无 schema 报错）。**sse 拒绝只由二进制 `ServerSchema` 静态证据支撑，非交互模式下 OmO 未显式报错**，故按 fail-closed 选择 plan-error。证据：`tasks/ongoing/senv-omo-agent-support/evidence/smoke-config.md`
- [x] 3.6 `go vet ./...`、`go test ./... -count=1` 全绿
- [ ] 3.7 commit（跟随仓库 log 风格，说明写原因）；OpenSpec archive 走单独流程
