---
name: senv-cli
description: 使用 senv client 的 CLI/MCP 安全读写环境变量、文本块、配置文件，管理分组、SSH 资产与 LLM Provider，并为 agent 配置 senv 接入。当任务涉及本机 senv 数据、senv CLI/MCP 工具或 senv 命令开发时使用。
metadata:
  version: "1.2"
---

# senv：agent 使用指南

senv 是本仓库的 CLI：AES-256-GCM 加密存储环境变量（env）、文本块（text）、配置文件（config），按 group 组织，支持 `{{env:group:key}}` / `{{text:group:key}}` 交叉引用。数据目录可用 git 同步（git provider），也可接入 senv-server（server provider，见下文）。

## 维护约定（给开发本仓库的 agent）

- 本文档是 agent 使用 senv 的权威说明。**新增/修改/删除 `cmd/` 下用户可见的命令、flag、交互/安全约束，或 MCP 工具清单与语义变化时，必须同步更新本文档**，并与功能变更一并提交。
- 事实来源：`senv --help`、`senv <cmd> --help`、`senv mcp list-tools`。不要凭记忆写命令用法。本机已安装的二进制可能落后于仓库 HEAD，开发本仓库时以 `go run . <cmd> --help` 为准。
- 子命令 help 无法覆盖的行为（交互提示、明文落盘、外部配置改写、凭据是否进入 argv/config）以实现、测试和 ADR 为准；有变化时也要回写本指南。

## agent 的两条访问路径

1. **MCP（优先）**：若宿主 agent 已配置 senv MCP server，直接调用 `senv` 前缀工具。当前共 20 个：env/text/group/config 各一组，另有只读 `ssh_host_list/get`、`llm_provider_list`、`llm_agent_status`。完整清单与描述以 `senv mcp list-tools` 为准。MCP server 无法提示密码；读写前确认用户已启动 session。
2. **CLI 兜底/管理面**：直接执行 `senv ...`。CLI 覆盖 MCP 不暴露的敏感管理操作，例如 keypair 导入/materialize、LLM Provider 写入与 coding agent 切换。

给 agent 安装 MCP 接入：`senv mcp install <claude-code|claude-desktop|cursor|codex|zcode|kimi|pi>`。先 `--print` 检查配置；只有用户明确要求时再落盘。`--all` 安装全部，`--scope project` 部分支持项目级配置。安装后需重启宿主 agent 生效。

## 非交互执行规则（重要）

- senv 解密需要密码，提示走 TTY。**管道/脚本环境里任何可能触发密码提示的命令都会卡住**——执行前先 `senv session status` 确认有活跃会话；没有会话就停下来让用户 `senv session start`，不要尝试替用户输密码。
- 需要 env 注入 shell 时用 `eval "$(senv env export --if-session)"`：无会话时静默退出 0，不会卡。
- 这些命令是交互式的，agent 不要用：`senv tui`、`senv interactive`、`senv config edit`、不带值/不带 `--file` 的 `senv text set`（TTY 下会开编辑器）、根快捷 `senv <group:key>` 不带值（同样可能开编辑器）。
- headless/CI 无安全内存存储时需 `senv session start --insecure-cache`（密钥落盘 0600），仅在用户明确要求时使用。
- 需要凭据的 LLM Provider 命令默认走 TTY prompt；非交互场景使用管道 stdin 或 `--key-ref`，不要把凭据放进 argv、日志或回复。
- `senv mcp install`、`senv ai switch`、`senv keypair materialize`、删除/覆盖/force push 都会写本机或外部状态。除只读查询外，先确认用户明确要求；不确定时先用 dry-run、`--print`、list/get 验证。

## 关键行为

- **寻址**：多数命令接受 `group:key` 地址（如 `prod:API_KEY`），地址中的 group 优先于 `-g/--group`。
- **快捷写入的两种语义**：根命令 `senv <group:key> [value]` 是 **text 写入**（如 `senv notes:TODO "内容"`）；env 写入的快捷形式是 `senv env <group:key> <value>`（如 `senv env prod:API_KEY "sk-xxx"`）。不带值的根快捷形式会走 stdin/编辑器，agent 避免使用。
- **引用解析**：存储值可含 `{{env:g:k}}` / `{{text:g:k}}`。`get` 默认原样输出，加 `-d/--decode` 解析；解析失败报错，加 `--loose` 保留未解析引用。`env export` 与 MCP `senv_env_export` 自动解析。
- **text set 输入优先级**：`--file` > stdin 管道 > 参数 > 编辑器。agent 写入文本块用 `--file` 或管道，避免触发编辑器。
- **最小暴露**：`env list` 会输出 `key=value`（值超过 50 字符截断），MCP `senv_env_list` 返回完整 key→值映射；`text list` 只显示 key、大小、更新时间。不要把 list 输出或密钥值复述进日志、回复。
- **默认分组**：未指定时用 `default`；`env export` 只导出已 activate 的 env 分组（`senv env group activate <name>`）。

## SSH 资产

- `keypair` 只导入既有 private key，不生成新密钥：`senv keypair import <name> --file <private-key>`；`list` 只看指纹/元数据。
- `keypair materialize <name>` 会把 private key 明文写到 `~/.ssh/senv/<name>`（目录 0700、文件 0600）。仅在用户明确要求时使用；删除 vault 记录不会自动删除已落盘文件。
- `host` 管理结构化连接档案，可引用 keypair：`senv host add web --hostname ... --user ... --port ... --keypair web-key`；`--attr`/host `extra` 按 OpenSSH 原样直传，不要接受不可信值。
- `senv host export [--host web] [--output <file>]` 渲染 OpenSSH config 片段，不输出 private key。写文件前先向用户确认目标路径。

## LLM Provider 与 coding agent

- 公共目录操作不需要解锁 vault：`senv ai refresh`、`senv ai catalog status`、`senv ai status`。
- 档案与凭据存 vault：`senv ai provider add/list/show/remove`。`show`/`list` 不返回凭据明文；`add` 禁止 `--api-key`，用 TTY prompt、`--api-key-stdin` 或 `--key-ref env:<group>/<key>`。HTTP base URL 必须显式 `--allow-http`。
- `senv ai switch <claude-code|codex|kimi|pi|opencode> <provider> [--model <model>]` 会事务式改写目标 coding agent 的原生配置并保存本机指向。切换后多数 agent 配置中会出现解密后的 API key（文件 0600）；Codex 只写环境变量名。仅按用户指定的 agent/provider/model 执行。
- MCP 只提供 provider 档案与 agent 指向的只读查询；不能通过 MCP 添加 provider 或切换 agent。

## server 模式

git provider 之外，vault 可托管在 senv-server 上：

- 接入：管理员签发一次性注册码 → `senv server register --address <url> --code <code>`；或全新机器直接 `senv init --server <url>`（token 默认取 `SENV_SERVER_TOKEN`）。vault 密码永不上传。
- 同步：`senv sync`（server provider 为增量 pull + 按条目乐观锁 push）。冲突时默认不改任何一侧，用 `--accept-remote`（以远端为准）或 `--force-push`（以本地为准）解决；`--no-interactive` 禁用交互式解决器。
- 历史与恢复：`senv history [kind:group:key]`（如 `senv history env:prod:API_KEY`）查看 server 保留的密文历史，`--restore <revision>` 恢复（会产生新 revision）。仅 server 模式支持；git 模式用 `git log`。
- 迁移：`senv migrate to-server` / `from-server` 在本地 git vault 与 server vault 间迁移。

## 常用命令速查

```bash
senv session status                      # 有活跃会话才能非交互读写
senv env get prod:API_KEY                # 取值（默认 raw；加 -d 解析引用）
senv env set prod:API_KEY "sk-xxx"       # 写入（等价 -g prod API_KEY ...）
senv env list [prod]                     # 输出 key=value（截断），勿复述进日志
senv text set --file notes.md docs:README
senv text get -d docs:README
senv config list && senv config export <name> [--path <target>]
senv config create <name> --source <file> --target <path>
senv config install [--all|--group g] --dry-run   # 确认计划后加 --yes 执行
senv env group list | add | activate | deactivate
senv git sync                            # commit + pull --rebase + push 数据目录
senv push -m "msg"                       # 等价 git add+commit+push；--only 只 push
senv sync                                # 按 settings 走 git 或 server provider
senv doctor                              # metadata 与数据文件密钥一致性诊断（git pull 后可跑）
senv audit --since 2026-09-01            # 本机审计日志（只记元数据不记值）
senv keypair list                        # 只看 SSH keypair 元数据/指纹
senv host list                           # 只看 SSH host 连接元数据
senv ai status                           # 查看 coding agent 的 provider/model 指向
```

完整清单以 `senv --help` 与 `senv mcp list-tools` 为准；本文档滞后时以命令输出为准并回写修正。
