# 配置源档案随同步通道分发，本机派生状态不同步

senv 的同步通道（`internal/syncschema`）此前只覆盖 env/env_meta/text/config/config_index 五类，人工添加进 vault 的 LLM Provider 档案与 MCP Server 档案不在内，结果是同一份配置在多台机器之间必须各自重配。我们把这"Onboarding 即同步"的边界正式化：**人工添加的"配置源"跨机同步，本机派生的"本机状态"不同步**——两类数据形态上都是加密 blob，差别不在存储而在语义归属。具体决策：syncschema 白名单扩到七 kind（`llm_provider`/`mcp_server`，身份=别名，与磁盘文件名一一对应）；档案本体走与 env/text 完全相同的加密 blob 通道（零知识不变式不变，服务端只见密文）；**凭据本体不出机**——LLM 档案只存 `credential_ref`（指向 vault text 组 `llm-keys`），MCP 档案的 secret 以 `{{env:...}}`/`{{text:...}}` 模板随档案同步、在导出端解析；Coding Agent 切换指针与 MCP 导出台账保持 per-machine（ADR-0003/0007 不变）；`senv ai switch` 凭据缺失 fail-closed 并诊断缺失条目，`senv mcp export` 引用缺失改为宽松写入（保留模板字面量 + 逐条 warning，与 ADR-0008 的明文落盘事实并存）。

## Considered Options

- **全部不同步（维持手工重配）**： rejected——配置源与 env/text 同形态，不同步的代价是每台新机器重复 `ai provider add` / `mcp add`，且无法用脚本化流程弥补（档案在 vault 里）。
- **全部同步（含切换指针、导出台账、agent 配置文件）**： rejected——这些是"切换/导出动作的本机结果"，指向哪台机器的哪些文件、导出到哪个 agent，天然 per-machine；同步它们会制造跨机语义冲突且违背 ADR-0003/0007 的既有边界。
- **按数据形态分界（采纳）**： 事实型"人工录入的配置源"同步，动作型"本机派生状态"不同步。分界线写在数据归属上而非存储形态上，故 `ssh_host`/`ssh_keypair`（同形态但属 SSH 资产管理面）**已识别但延后**——待 SSH 资产多机场景成立时以同一机制接入，本 ADR 不预支该决策。

## Consequences

- 多端混跑旧版本 senv 时，新 kind 的档案不会被旧客户端收集/落地（白名单拒绝），不损坏本地状态；升级即恢复。
- 配置源条目的冲突走既有 revision 乐观锁与冲突解决流程，报告对这两个 kind 追加"本地/远端 alias+revision 对照"提示——双端人工修改配置源更可能是有意义的分歧，需要人裁决而不是自动覆盖。
- 凭据引用在本机解析失败是常态而非异常（新机器先到档案、后补凭据）：ai switch 保持 fail-closed + 指明缺失条目；mcp export 宽松写入让档案先落地，凭据补齐后重跑导出即收敛。
- `ssh_host`/`ssh_keypair` 接入通道是低风险后续项，但需独立评审（密钥资产的同步半径与 ADR-0001/0002 的相互作用）。

部分由 [ADR-0020](./0020-ssh-assets-in-sync-channel.md) 落地：`ssh_host`/`ssh_keypair` 已裁决接入，白名单现为九 kind，私钥随档案 blob 跨机。本文「凭据本体不出机」修正为「档案 blob 不内嵌凭据」——`llm-keys` 凭据与被 `{{text:...}}`/`{{env:...}}` 引用的条目本就随 text/env 通道同步，延后项的评审结论见 ADR-0020。
