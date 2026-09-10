# 0003-local-agent-pointer

Coding Agent 的当前指向（`(provider, model)`）是本机状态，存 `~/.config/senv/agent-pointers.json`，刻意不进 vault、不随同步分发。agent 配置文件本身是本机的，同步指针会与本机实况脱节：两台机器各自 `switch` 后指针必然分叉，vault 里的「唯一指针」反而失去意义。代价是 `senv ai status`/TUI/MCP 展示的是「senv 最近一次切换的指向」而非 agent 配置的实况（用户手工改配置后 senv 不感知），这是单向指针模型的既定边界。
