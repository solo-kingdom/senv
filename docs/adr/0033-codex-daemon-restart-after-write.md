# codex 写回后按需重启 app-server daemon

`senv ai switch codex` 写 `~/.codex/config.toml` 的 `env_key` 与 provider 段，`senv mcp export` / `senv mcp install codex` 写 `mcp_servers` 表。这些改动**不会**被已经在跑的 `codex app-server` daemon 读到：codex 在 daemon 启动时把配置与环境各拷贝一次，之后不再刷新（官方 README：「Shared clients use the environment inherited when the daemon started」）；源码 `app-server-daemon/src/backend/pid_start.rs` 也是 `background_command(...).env(...)` 从当前进程派生，属一次性继承。

此前只在 [ADR-0024](./0024-codex-credential-env-name.md) 与 senv-cli skill 里记成「已知坑，手动 `pkill -f 'codex app-server'`」。结果是切换成功、TUI 仍报 `Missing environment variable` 或仍用旧 provider——静默坏状态，与 ADR-0019 的 fail-closed 取向相反。

现在：命中 codex 的写回成功后探测本机 daemon，在跑就**询问**是否执行 `codex app-server daemon restart`；探测不到则完全静默。

## 决策

1. **按需提示，不自动重启。** daemon 是共享进程（Desktop / SSH 远程客户端共用），重启会打断正在进行的任务，且 60s 宽限期内可能强杀。因此只作为写回后的可选项，由用户 `y`/`enter` 确认。
2. **探测不到就静默。** 没有 daemon 时没有可重启的东西，提示纯属噪音。判定用 `codex app-server daemon version` 的 `status == "running"`；二进制不存在、老版本 CLI 不认该子命令、退出码非 0 一律视为「没有 daemon」，只有探测本身超时才上报。
3. **重启由 senv 发起，并注入 `senv env export` 的解析结果。** daemon 继承发起进程的环境；若在从没 eval 过 export 的 shell 里重启，新 daemon 会缺 `env_key` 指向的变量，反而从「旧配置」坏成「缺凭据」。因此重启时把 export 变量叠加到子进程环境（叠加而非替换：用户手导的 PATH / TLS 变量照旧）。
4. **注入失败不阻断重启。** 拿不到 vault、export 解析出错等情况照常重启，只是不注入——凭据失效是用户可见的报错，拒绝重启则等于让功能对未解锁场景完全不可用。
5. **非交互不代为重启。** `--yes`、`--print`、未接 TTY 时只打印一行提示（含命令），不替脚本决定重启共享进程。
6. **CLI 与 TUI 同一语义。** CLI 在 `ai switch` / `mcp export` / `mcp unexport` / `mcp install` 的成功路径上询问；TUI 由 LLM Tab 与 MCP Tab 在写回完成后发 `codexDaemonCheckMsg`，确认框由顶层 Model 统一渲染与处理——两个 Tab 不各复制一套状态，也自然复用 search/help 覆盖层的按键所有权。
7. **只在实际写盘时提示。** `--dry-run`、`--print`、计划被取消、全部项失败时都不提示：没有改动就没有要生效的东西。

## Considered Options

- **自动重启**：省一次确认，但可能在用户正用 Desktop 时打断任务；无法从 senv 侧判断 daemon 是否空闲。
- **只打印提示不执行**：实现最小，但把三步操作（重启 + 先 export + 保证环境）留给用户，正是本次要消灭的失败模式。
- **继承当前进程环境直接重启**：会静默丢失凭据，比不改更糟，见决策 3。
- **在 switch 内直接调 `pkill`**：会绕过 codex 自己的优雅关闭与 pid 记录清理，可能留下陈旧 pid 文件。
- **给 daemon 写 `~/.codex/.env`**：codex 只在启动时读它，且会把 senv 的凭据明文落到另一个文件（违背 ADR-0002 的落盘约束）。

## Consequences

- 切换/导出在交互终端上多一次 y/N；`n` 跳过时不改变已完成的写回。
- 注入的变量进入 daemon 进程环境（`/proc/<pid>/environ`），与用户手动 `eval "$(senv env export)"` 后重启等价，暴露面不变。
- daemon 重启会中断进行中的远端会话；确认框文案已点明。
- 探测每次多起一个 codex 子进程（实测 ~30ms），且只在命中 codex 的写回后触发。

## Status

accepted（2026-10-28）
