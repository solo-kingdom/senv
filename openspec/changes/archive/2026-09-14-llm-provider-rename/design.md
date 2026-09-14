## Context

See proposal.md — Why。现状：`ProviderManager` 以 alias 为文件名主键（`llm_providers/<alias>.enc`），自有凭据落在 `texts/llm-keys/<alias>.enc`，本机指针在 `~/.config/senv/agent-pointers.json`；`EditProvider` 明确禁止改 alias。keypair 已有「同次 mutation 改名 + 联动引用」先例（`RenameKeyPair`），本设计对齐该模式。

## Goals / Non-Goals

**Goals:**
- CLI + TUI 统一走同一 rename API。
- 档案、规范自有凭据、本机指针在一次用户操作内联动；冲突 fail-closed。
- 明确不触碰 agent 原生配置，靠重跑 switch 收敛。

**Non-Goals:**
- 不在 rename 路径调用各 agent 适配器写盘。
- 不迁移默认 env 组里由 switch 播种的 `SENV_<ALIAS>_API_KEY` 条目。
- 不改变 server/git 同步协议（仍是「删旧密文 + 写新密文」的条目级变更）。

## Decisions

### D1: 独立 `rename` 子命令，不扩展 `edit`
- **选择**：`senv ai provider rename <old> <new>`；`edit` 继续拒绝改 alias。
- **理由**：与 keypair/env/text/config 一致；避免把主键迁移塞进局部字段 patch。
- **备选**：`edit --alias` —— 易与「只改传入字段」语义冲突，否决。

### D2: 编排顺序与回滚
数据流：

```
校验 old 存在、new 合法且档案/目标自有凭据不冲突
        │
        ▼
若自有凭据 text:llm-keys/<old> 存在 → RenameText(llm-keys, old→new)
若缺失自有凭据 → 跳过 text，仍更新 credential_ref 到新规范引用
        │
        ▼
写新档案 llm_providers/<new>.enc（alias/credential_ref/updated_at 已更新）
删除旧档案 llm_providers/<old>.enc
        │
        ▼
LoadPointers → 改写 provider 字段 → SavePointers（文件缺失则跳过）
        │
        ▼
输出：指针数 + 提醒 re-switch
```

- **失败策略**：任一步失败时，已完成的 vault 步骤尽量逆操作恢复（text 已改名则改回；新档案已写则删新并保留/恢复旧档案）。指针写失败时 vault 已成功则返回错误并说明档案已改名、指针未更新、可手工改指针或重试（指针路径独立于 vault，与 switch 在配置写成功后再写指针的不对称类似；优先实现「指针失败也尝试报告清晰」）。实现上优先把 vault 两步（text + provider 文件）做成可恢复序列，指针作为最后一步。
- **备选**：真正的跨存储事务 —— 当前 vault 无跨 kind 事务，成本过高，否退。

### D3: 外部引用与 env 种子
- 外部 `credential_ref` 不移动。
- 已存在的 `SENV_<OLD>_API_KEY` env 种子不删除、不改名；重切 switch 会按新 alias 播种 `SENV_<NEW>_API_KEY`。旧种子成为无害残留（用户可手动删）。
- **备选**：rename 时同步改 env 种子 —— 易误伤用户同名条目，否决。

### D4: TUI
- provider 栏 `r` → 单字段表单（新别名）→ 调同一 `RenameProvider`。
- `e` 保持别名只读。
- 键位说明与 `?` 总览同源更新；底栏增加 rename。

### D5: 审计与文档
- 审计 `op_llm_provider`，target `provider:<new>`，detail 含 `rename <old>`。
- 同步 `.agents/skills/senv-cli/SKILL.md` 与 README。

## CLI 示例

```bash
senv ai provider rename acme acme-prod
# renamed acme → acme-prod; updated 2 agent pointer(s)
# note: coding agent configs still use senv-acme until you re-run: senv ai switch <agent> acme-prod

senv ai provider rename acme acme   # 同名：成功空操作或报「无需改名」（实现选其一并测稳）
senv ai provider rename acme taken  # error: provider "taken" already exists
```

## Risks / Trade-offs

- [Risk] rename 后 agent 仍指向旧 `senv-<old>`，用户以为已生效 → 命令/TUI 明确提示重跑 switch；status 显示新 alias 可能与原生配置短暂不一致（既有漂移提示可覆盖「指针模型不在档案」类问题，标识陈旧靠提示文案）。
- [Risk] text 改名成功、provider 文件写失败导致短暂不一致 → 逆改名 text / 恢复旧档案；测试覆盖该路径。
- [Risk] server 同步出现删旧+增新两条变更 → 可接受，与 keypair rename 同类；冲突时走既有 sync 解决。

## Migration Plan

- 无数据格式变更；旧档案无需迁移。
- 回滚：去掉 rename 命令与 TUI `r` 即可；已改名的用户数据保持新 alias。

## Open Questions

（无）
