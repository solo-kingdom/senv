## Context

- CONTEXT.md "本地状态"分组（解锁缓存/持久会话/临时认证）与同步边界的"本机状态"（当前指向/导出状态/agent 配置文件）同名不同物；grill 术语表已完成辨析，按其定义落 CONTEXT.md
- ADR 编号：按落地时 `docs/adr/` 扫描取下一空位，2026-09-12 复核为 0019
- skill 更新纪律：AGENTS.md 要求 cmd 行为变化时同一 change 内更新 `.agents/skills/senv-cli/SKILL.md`

## Decisions

- D-a CONTEXT.md 措辞："配置源"定义为独立术语条目（避免与"本地状态"分组冲突），并在"当前指向""导出状态"条目内显式写明"本机状态（同步边界语境）：不随 vault 同步"；不新建顶级分组（分组语义不动，改动最小）
- D-b ADR 结构：Context（配置源人工分裂之痛）/ Decision（两 kind 入通道、凭据引用出机而凭据本体不出机、本机派生态不同步）/ Considered Options（a. 全部不同步维持手工 b. 全部同步含本机状态 c. 按数据形态分界——采纳）/ Consequences（旧客户端混跑不落地新档案、ssh_host/ssh_keypair 已识别延后）
- D-c skill 更新位置：senv-cli SKILL.md 的 sync/export/switch 相关小节各补一段行为说明，不改命令用法结构

## Open Questions

无
