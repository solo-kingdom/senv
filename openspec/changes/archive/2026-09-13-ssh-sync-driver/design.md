## Context

同步通道的收集/落地都收敛在 server provider 的本地缓存层：`internal/provider/server_state.go` 的 `collectEntriesDiff`（vault 目录 → 同步条目）与 `entryLocation`（kind → 落盘路径），白名单在 `internal/syncschema`，被 client（收集、落地、远端身份校验）与 server（`internal/server/store/store.go` 事务前整批校验）共享。SSH 资产已有完整的加密 blob 存储（`storage/ssh.go`：`hosts/`、`keypairs/` 目录，文件名=别名+`.enc` 后缀）与 CRUD/导出面，只缺通道接入。设计决策已定稿于 ADR-0020，本文不再重复动机（见 proposal.md - Why）。

git 模式（`internal/provider/git.go`）是对 vault 目录整体的 git push/pull，SSH 资产文件天然随仓库分发，不经白名单，本 change 无需触及。

## Goals / Non-Goals

**Goals:**

- `ssh_host` / `ssh_keypair` 两个 kind 走通「收集 → push → server 校验 → pull → 落地」全链路
- SSH 资产在冲突报告、审计输出中与其他配置源同等呈现（alias+revision 对照），但绝不渲染明文
- `host export` 对缺失 keypair 引用给出可见 warning

**Non-Goals:**

- 见 proposal.md - Non-goals（无 server 代码改动、无 TUI 改动、不动 git 模式与 ADR-0001 边界）

## Decisions

### D1 kind 命名与身份：`ssh_host`/`ssh_keypair`，身份=别名

沿用 ADR-0019 行文已用的名字；grp 为空、key=`securefs.ValidateSegment` 校验的别名，与 `llm_provider`/`mcp_server` 完全同构。备选 `host`/`keypair` 被否：与既定术语冲突，跨 ADR 检索困难。

### D2 收集/落地：复用配置源档案的目录遍历模式

`collectEntriesDiff` 按既有 `llm_providers/`/`mcp_servers/` 的写法追加两个遍历段（仅 `.enc` 后缀、目录缺失静默跳过）；`entryLocation` 加两个 case 映射回 `{HostDirName|KeypairDirName, key+ConfigFileSuffix}`。零新协议、零存储格式变更。

### D3 冲突呈现：元数据 only + 配置源提示

`ssh_host`/`ssh_keypair` **不加** `internal/conflict/render.go` 的明文解码分支（env/text 有、llm/mcp/ssh 没有），渲染停留在 revision/deleted/size 元数据层；`cmd/sync.go` 的 `isConfigSourceKind` 扩入两个新 kind，使冲突报告带「本地/远端 alias+revision 对照」提示。理由：`KeyPairEntry.PrivateKey` 是私钥本体，任何渲染文本不得出现私钥明文（ADR-0005 红线、ADR-0020 定案）；裁决所需元数据已足够。

### D4 export 缺失引用：宽松写入 + 逐条 warning

`host export` 在 `IdentityKey` 引用的 keypair 不在本机 vault 时逐条 warning、照常写出 config（`mcp export` 宽松写入同模式，ADR-0019 已把「档案先到、依赖后补」定为常态时序）；`keypair materialize` 维持 fail-closed。备选 fail-closed 被否：会让 host 先落地的机器完全无法导出 config。

### D5 切片拆分：channel（通道核心）+ export-warning（用户可见行为与文档）

两个子 change 文件范围不重叠（channel：syncschema/provider/conflict/cmd+server store 测试；export-warning：internal/ssh+SKILL.md+ssh-assets spec），可串行实施，理论上可并行。不进一步细分：channel 内部 syncschema→collect→land→conflict 是同一契约链，拆开会产生中间态白名单不一致。

## 数据流

```
本机 vault                          server（零知识存储）
hosts/<alias>.enc ─┐
keypairs/<name>.enc ─┤ collectEntriesDiff ── push ──▶ syncschema.ValidateIdentity（事务前整批）
                    │  （新增两个遍历段）              │ 通过 → 按 (vault,kind,grp,key) 存密文
                    │                                └ 拒绝 → 整批失败（旧 server 场景）
远端条目 ◀── pull ──┘
  └─ syncschema.ValidateIdentity → entryLocation（新增两个 case）
       → hosts/<alias>.enc / keypairs/<name>.enc 落盘（0600）
       → TUI ssh tab / CLI 经 reloadAllTabs / 常规读路径可见
```

## 错误处理策略

- **fail-closed 点**：身份校验（client 收集前、client 落地前、server 事务前，三层同一 `syncschema`）；`keypair materialize` 对缺失 keypair 维持现状报错带名字。
- **宽松点**：`host export` 缺失引用 → 逐条 warning + 照常写出（D4）。
- **冲突路径**：SSH 条目与其他 kind 同走 revision 乐观锁；报告只出元数据（D3），不因解码失败产生新错误面。

## 向后兼容

存储格式零变更（同样的 SSH-style 加密 blob，原路径读写）。混跑矩阵：

- 新 client + 新 server：全量 SSH 资产同步。
- 旧 client + 新 server：旧 client 收集不到新 kind，行为不变、不损坏。
- 新 client + **旧 server**：携带 ssh 条目的整批 push 被 server 事务前校验**整批拒绝**（连带 env/text）→ 发布顺序必须 server 先行（见 Migration Plan）。
- git 模式：任何组合都随 git 仓库直接分发，不受白名单影响。

## Migration Plan

1. 合并后先构建并发布 senv-server 镜像（`docs/senv-server.md` 流程，优先 iship 构建），使 server 白名单先行扩容。
2. 再发布 client。已升级 client 在旧 server 上的表现为整批同步失败、错误指向 kind 校验——回滚 client 即恢复。
3. 回滚：revert 白名单相关提交即可，server 上已存的 ssh 条目成为无害孤儿数据（零知识存储不解释），重新升级后自动恢复同步。

## Risks / Trade-offs

- [私钥明文经渲染/日志泄露] → D3 元数据 only + 渲染输出断言私钥字符串不出现的单测；审计输出沿用既有 kind/grp/key 标识不含明文。
- [新 client + 旧 server 整批同步失败被误报为故障] → Migration Plan 固定 server 先行；错误信息沿用 server 既有 kind 校验文案，doctor/冲突报告不特判。
- [host 先到、keypair 未到的悬空引用] → D4 export warning 提前可见；materialize fail-closed 兜底。
- [遍历两个新目录增加收集开销] → 复用既有 mtime+size 增量快照机制，未变更文件不重读。

## Open Questions

无——设计已在 ADR-0020 定稿，本文仅做实现映射。
