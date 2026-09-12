## Context

收集与落地收敛在 `internal/provider/server_state.go`（`collectEntriesDiff` / `entryLocation`），白名单在 `internal/syncschema`，client 与 server（`internal/server/store/store.go` 事务前整批校验）共享。SSH 档案已是同构的 SSH-style 加密 blob（`hosts/<alias>.enc`、`keypairs/<name>.enc`），本 change 只接通道，不动存储。动机见 proposal.md - Why；切片编排见 driver `../ssh-sync-driver/design.md`。

## Goals / Non-Goals

**Goals:**

- 九 kind 白名单下，「收集 → push → server 校验 → pull → 落地」对两个 SSH kind 全链路打通
- 冲突呈现：SSH 条目享受配置源对照提示、但不解码明文

**Non-Goals:**

- host export 行为（`ssh-sync-export-warning` 切片）；server 代码改动；git 模式；TUI

## Decisions

### D1 kind 命名与身份
`ssh_host`/`ssh_keypair`，grp 空、key=别名。ADR-0019 已用此名；与 `llm_provider`/`mcp_server` 的 alias 模式同构。备选 `host`/`keypair` 否——与既定术语冲突。

### D2 收集/落地复用配置源目录遍历模式
`collectEntriesDiff` 追加两个遍历段（仅 `.enc` 后缀、目录缺失静默跳过），`entryLocation` 加两个 case 映射回原路径。零新协议、零存储格式变更。备选「为 SSH 单独建同步面」否——无收益且制造两套语义。

### D3 冲突呈现：`isConfigSourceKind` 扩容但不解码
`cmd/sync.go` 的 `isConfigSourceKind` 扩入两新 kind（对照提示行复用既有文案，alias=key）；`internal/conflict/render.go` **不加**解码分支。理由：`KeyPairEntry.PrivateKey` 是私钥本体，ADR-0005 红线 + ADR-0020 定案；元数据（别名/revision/删除/大小）足够裁决。测试必须断言渲染输出不含档案明文。

## 数据流

```
hosts/<alias>.enc ──┐ collectEntriesDiff（新增遍历段）
                    ├ ── Entry{Kind: ssh_host|ssh_keypair} ── push ──▶ server 事务前 ValidateIdentity（共享白名单）
keypairs/<name>.enc ┘                                                        │
远端条目 ◀── pull ── validateIdentity ── entryLocation（新增 case）落回原目录 ◀┘
```

## 错误处理策略

fail-closed 三层同一白名单：client 收集前（不产生非法身份）、client 落地前（`entryLocation` 先校验再触盘）、server 事务前（整批拒绝）。冲突路径不新增错误面：SSH 条目走既有 revision 乐观锁，报告只出元数据。

## 向后兼容

存储格式零变更。混跑矩阵与发布顺序（server 先行）见 driver design「向后兼容 / Migration Plan」：新 client + 旧 server 时整批 push 被拒、回滚 client 即恢复；旧 client + 新 server 仅收集不到新 kind。

## Risks / Trade-offs

- [渲染/日志泄露私钥] → D3 元数据 only + 渲染输出断言私钥字符串不出现的单测
- [新 kind 增加收集开销] → 复用 mtime+size 增量快照，未变更文件不重读
- [server store 测试遗漏新 kind] → 对齐测试显式覆盖接受/拒绝两侧

## Open Questions

无。
