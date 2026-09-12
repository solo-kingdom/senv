## Why

本 driver 把"配置源同步、本机状态不同步"的边界正式化了，但这条边界目前只存在于决策记录里：CONTEXT.md 的"本地状态"分组与同步边界的"本机状态"是同名不同物（grill 术语表已辨析），需要显式定义"配置源"术语并区分；ADR 候选 `sync-ai-mcp-source-of-truth` 满足三门槛（难逆转：kind 白名单与通道归属；后人费解：为何 AI/MCP 档案与 env/text 同通道而指针/ledger 不同步；真实取舍：全同步 vs 全本机的路线之争），转正落盘；senv-cli skill 需反映宽松 export 与新 kind 的同步行为。

## What Changes

- `CONTEXT.md`：新增"配置源"术语（可随 vault 跨机分发的人工添加数据：env/text/config、LLM Provider 档案、MCP Server 档案）；在"当前指向""导出状态"等既有条目处显式标注其属"本机状态（同步边界）"并与"本地状态"分组区分；不触动 ADR-0003/0007/0012 边界
- `docs/adr/0019-sync-ai-mcp-source-of-truth.md`（proposed；编号按落地时 `docs/adr/` 扫描取下一空位）：记录两个 kind 入通道、本机派生态不同步的决策与 ssh_host/ssh_keypair 延后项
- `.agents/skills/senv-cli/SKILL.md`：补充 `llm_provider`/`mcp_server` 随 sync 分发的行为、`senv mcp export` 引用缺失时的宽松写入 + warning、`senv ai switch` 凭据缺失诊断

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

（无——纯文档与 agent skill，`skip_specs: true`）

## Impact

- 三个文档文件；无代码、无行为面

## Non-goals

- 不改任何代码行为；不动既有 ADR-0002/0003/0006/0007/0008/0012

## 安全性分析

不适用（文档变更；ADR 内容涵盖既有安全边界陈述，不引入新敏感面）。
