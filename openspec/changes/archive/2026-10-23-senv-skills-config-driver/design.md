# Design

## Context

See proposal.md — Why。冻结方案在台账 `~/code/tasks/ongoing/senv-skills-config/design/skills-config.md` 与 ADR `adr-skill-kind-encrypted-tree.md`。本文件只记 driver 级实现边界与切片切法，不重复全文。

现状要点：无 `skill` kind；config 单文件；`MaxEntryCiphertext=512KiB`；rekey/orphan 不认识未知 `.enc`；git sync 可测 A/B；CLI 配置目录默认走 `user.Current()`（验收必须注入路径）。

## Goals / Non-Goals

**Goals:**
- 落地新 kind + 加密目录树 + `senv skill` CLI + 双根 apply + 判据 Go 测试
- rekey / doctor / orphan / syncschema（及 server 路径收集）认识 skills
- 文档与 senv-cli 真源 + `~/code/.agents/skills/senv-cli` 镜像

**Non-Goals:**
- 同 proposal Non-goals；不在本 driver 改共享线上 server 部署

## Decisions

1. **切片**：`core`（存储/身份/CRUD/rekey·orphan/server 上限）→ `apply-e2e`（apply + 判据测试）→ `docs`。理由：apply 依赖入库；e2e 依赖 apply；文档最后对齐行为。
2. **代码根**：`worktrees/senv-skills-config`（Driver 协议）；台账仍在 `~/code/tasks`。
3. **技术选择**：沿用台账 ADR（ustar+Encrypt、id 全局唯一、UserHomeDir、tar 白名单、明文 index 在数据目录）。

## Risks / Trade-offs

- [旧 server 拒未知 kind] → 文档写先升级；e2e 用 git，不碰线上 server
- [64MB 请求体 vs 大批 1.5MiB skill] → 文档说明；client 分批可后置
- [与 dotf 同名] → skip 非托管/软链；不调用 dotf

## Migration Plan

- 纯新增 kind；无旧数据迁移
- 回滚：删 skill 相关代码与已同步条目；已 apply 目录保留

## Open Questions

- 台账未决：kiro、`unapply`、TUI/MCP、pointer（不挡切片）

## 数据流（简）

```
源目录 --add--> tar --Encrypt--> skills/<group>/<id>.enc + skill_index.json
                                      |
                                   senv sync
                                      |
                                      v
目标 vault --apply--> 解密 tar --> $HOME/.agents|/.claude/skills/<id>/ (+ .senv-owned)
```

## 错误处理

- 超限 / 缺 SKILL.md / name≠id / tar 非法类型 → 拒绝写入，明确错误
- apply 遇软链或非托管目录 → 该根 skip + warning，其它根继续
- sync 冲突 → 沿用既有 env/text/config 冲突策略（实现期对齐最近似 kind）

## CLI 示例

```bash
senv skill add demo --source ./fixtures/demo-skill
senv skill list
senv skill apply demo --dry-run
senv skill apply demo --yes
senv sync
```
