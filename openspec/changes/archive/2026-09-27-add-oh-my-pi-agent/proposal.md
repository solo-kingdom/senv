# oh-my-pi（omp）agent 支持

## Why

oh-my-pi（omp，can1357/oh-my-pi）是 Pi 的活跃 fork，本机与社区使用在增长，但 senv 的 llm/mcp 两侧都不认识它。senv 现有 `pi` 目标写 `~/.pi/agent`，omp 根本不读（配置面已分化为 `~/.omp/agent` + YAML）；不为 omp 单列目标等于把用户推向手工配置，丢失「senv 是唯一事实源」的边界。

## What Changes

- mcp 注册表新增目标 `omp`（Oh My Pi）：内置 MCP 无前置依赖；user `~/.omp/agent/mcp.json`、project `.omp/mcp.json`；remote 写 `type` 键 + headers；别名 `oh-my-pi` 仅查找接受。
- llm 切换新增 `omp` 适配器：写 `models.yml`（provider 条目，投影复用 pi 适配器字段）+ `config.yml` 的 `modelRoles.default`；新增 `applyYAMLMerge` 写回原语（JSON/TOML/YAML 三原语并列）。
- `OmpAgentDir` 镜像 omp 路径解析：`OMP_PROFILE`/`PI_PROFILE`、`PI_CONFIG_DIR`、`PI_CODING_AGENT_DIR`（仅默认 profile）；XDG 不支持。
- 凭据 CredentialInline（0600），与 pi 一致；只写 `modelRoles.default`，不动 smol 等角色。
- 列表/帮助/报错只显示规范 id `omp`；TUI 两处 agent 清单注册表驱动、零改动。

非目标：不支持 omp 的 XDG 重定向；不用 `apiKey` 环境变量名/`!cmd` 间接取密；不写 `modelRoles` 的 smol/slow/plan 角色；不改 pi 目标的任何行为。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `llm-provider-switch`: 受支持 agent 注册表加入 omp；新增 omp 的 YAML 写回要求（models.yml provider 条目 + config.yml modelRoles.default）；枚举连带的凭据/地址/兼容性条款同步。
- `mcp-server-export`: 受支持导出/安装目标加入 omp（内置 MCP、user/project 双路径、remote type 键）；别名查找与展示面规则。

## Impact

- 代码：`internal/agentcfg/agents.go`（新目标 + `OmpAgentDir` + Aliases）、`internal/llm/switch.go`（`ompAdapter` + `applyYAMLMerge` + Aliases）、两个注册表的查找函数。
- 依赖：`gopkg.in/yaml.v3` 从 indirect 转直接。
- 文档：`docs/adr/0030`（已随本 change 起草）、`.agents/skills/senv-cli/SKILL.md`。
- 安全：明文落盘面与 pi 相同（ADR-0002 取舍，0600 收敛），不扩大；fail-closed 凭据决议复用既有路径。

## 安全性分析

omp 适配器的凭据处理完全复用 pi 路径：解密引用 → 明文写入 senv 拥有的 `providers.senv-<alias>.apiKey`（文件 0600），同 ADR-0002 的既定取舍。`config.yml`/`models.yml` 重写丢注释，与 TOML 先例一致，不引入新的暴露面。别名仅影响查找，不改变写盘目标。
