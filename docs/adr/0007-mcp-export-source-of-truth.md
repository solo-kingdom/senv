# 0007-mcp-export-source-of-truth

MCP Server 档案存 vault，senv 是它的唯一事实源；`senv mcp export` 把档案按目标 Coding Agent 的配置格式合并写入其全局配置，agent 配置文件是派生产物，senv 不回读它作为事实源。导出的本机状态（agent → 别名 → 写入内容指纹）存 `~/.config/senv/mcp-exports.json`，与 LLM 指针同属本机状态、刻意不进 vault：两台机器各自导出后 agent 配置本就不同，同步台账会与实况脱节（同 ADR-0003 的理由）。代价是沿用单向模型——用户手工改过 agent 配置后 senv 不感知，指纹不符即判为漂移，默认拒绝覆盖，需 `--force`。

## Considered Options

- **不记任何状态，纯靠 `--force`**：不多一个本机文件，但每次手工漂移后都必须强制覆盖，把「这条是不是 senv 管理的」判断责任推给用户。
- **在 agent 配置里写 senv 标记**：可判定且不引入本机文件，但污染 agent 配置，且各家格式对未知键的容忍度不一致。
- **台账进 vault 随同步分发**：看似「一处配置到处可用」，但 agent 配置文件是本机的，反对理由与 ADR-0003 反对同步指针完全相同。
