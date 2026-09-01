---
name: senv-cli
description: 使用 senv（加密的环境变量/文本块/配置管理 CLI 与 MCP 服务）读写密钥、管理分组、同步数据。当任务涉及读取或写入本机的 senv 数据、调用 senv CLI/MCP 工具、或为 agent 配置 senv MCP 接入时使用。维护者注意：senv 命令或 MCP 工具变更时必须同步更新本文档。
metadata:
  version: "1.0"
---

# senv：agent 使用指南

senv 是本仓库的 CLI：AES-256-GCM 加密存储环境变量（env）、文本块（text）、配置文件（config），按 group 组织，支持 `{{env:group:key}}` / `{{text:group:key}}` 交叉引用，数据目录可用 git 同步。

## 维护约定（给开发本仓库的 agent）

- 本文档是 agent 使用 senv 的权威说明。**新增/修改/删除 `cmd/` 下的命令、flag，或 `mcp` 工具列表变化时，必须同步更新本文档**，并与 PR 一并提交。
- 事实来源：`senv --help`、`senv <cmd> --help`、`senv mcp list-tools`。不要凭记忆写命令用法。

## agent 的两条访问路径

1. **MCP（优先）**：若宿主 agent 已配置 senv MCP server，直接用 `senv_*` 工具（16 个，见 `senv mcp list-tools`），如 `senv_env_get`、`senv_text_set`、`senv_config_export`、`senv_group_list`。
2. **CLI 兜底**：直接执行 `senv ...` 命令。

给 agent 安装 MCP 接入：`senv mcp install <claude-code|codex|cursor|...>`（`--print` 只打印配置不落盘，`--all` 安装全部）。

## 非交互执行规则（重要）

- senv 解密需要密码，提示走 TTY。**管道/脚本环境里任何可能触发密码提示的命令都会卡住**——执行前先 `senv session status` 确认有活跃会话；没有会话就停下来让用户 `senv session start`，不要尝试替用户输密码。
- 需要 env 注入 shell 时用 `eval "$(senv env export --if-session)"`：无会话时静默退出 0，不会卡。
- `senv tui`、`senv interactive`、不带值的 `senv text set`（会开编辑器）都是交互式的，agent 不要用。

## 关键行为

- **寻址**：多数命令接受 `group:key` 地址（如 `prod:API_KEY`），地址中的 group 优先于 `-g/--group`。快捷写入：`senv prod:API_KEY "sk-xxx"`。
- **引用解析**：存储值可含 `{{env:g:k}}` / `{{text:g:k}}`。`get` 默认原样输出，加 `-d/--decode` 解析；解析失败报错，加 `--loose` 保留未解析引用。`env export` 自动解析。
- **text set 输入优先级**：`--file` > stdin 管道 > 参数 > 编辑器。agent 写入文本块用 `--file` 或管道，避免触发编辑器。
- **最小暴露**：`env list` / `text list` 只显示 key 不显示值；取值才用 `get`。日志、回显中不要复述密钥值。
- **默认分组**：未指定时用 `default`；`env export` 只导出已 activate 的 env 分组（`senv env group activate <name>`）。

## 常用命令速查

```bash
senv session status                      # 有活跃会话才能非交互读写
senv env get prod:API_KEY                # 取值（默认 raw；加 -d 解析引用）
senv env set -g prod API_KEY "sk-xxx"    # 写入
senv env list [-g prod]                  # 只列 key，不列值
senv text set --file notes.md docs:README
senv text get -d docs:README
senv config list && senv config export <name>
senv env group list / add / activate / deactivate
senv git sync                            # commit + pull --rebase + push 数据目录
senv doctor                              # metadata 与数据文件密钥一致性诊断（git pull 后可跑）
```

完整清单以 `senv --help` 与 `senv mcp list-tools` 为准；本文档滞后时以命令输出为准并回写修正。
