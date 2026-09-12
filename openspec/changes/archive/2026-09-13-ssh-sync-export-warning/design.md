## Context

`host export` 当前在 `internal/ssh/host.go` 生成片段时无条件写 `IdentityFile ~/.ssh/senv/<name>`（ADR-0001 落盘约定路径），不校验 keypair 是否在本机 vault。TUI 导出预览复用同一 `Export` 路径。设计定稿见 ADR-0020 D4；切片编排见 driver `../ssh-sync-driver/design.md`。

## Goals / Non-Goals

**Goals:**

- 悬空 `IdentityFile` 引用在导出时可见（逐条 warning），不阻断导出

**Non-Goals:**

- `materialize` 与 `proxyJump` 的 fail-closed 行为不变；MCP export、TUI 界面改造

## Decisions

### D1 宽松写入 + 逐条 warning（`mcp export` 同模式）
导出过程中对每个 host 检查 `IdentityKey` 引用的 keypair 是否在本机 vault（复用既有 keypair 列表/加载路径，一次收集避免逐 host 重复解密），缺失则输出 `warning: host <alias> 引用的 keypair <name> 不在本机 vault（可能尚未同步）` 到 stderr，片段照常生成。备选 fail-closed 否——会让 host 先落地的机器完全无法导出；备选静默 否——悬空要到 ssh 连接失败才暴露，失去提前可见的价值。

### D2 warning 走 stderr、片段走 stdout/文件
与既有导出输出流保持一致（`--output` 写文件时 warning 仍进 stderr 可见），不污染片段内容。

## CLI 行为示例

```
$ senv host export
warning: host web 引用的 keypair web-key 不在本机 vault（可能尚未同步）

Host web
  HostName web.example.com
  User deploy
  IdentityFile ~/.ssh/senv/web-key
```

## 错误处理策略

缺失引用是 warning 不是 error；`proxyJump` 悬空维持 MUST 报错（有意不对称：ProxyJump 错误会让 ssh 直接连接到错误目标，IdentityFile 缺失只是回退认证且可自愈）。

## Risks / Trade-offs

- [keypair 存在性检查增加导出开销] → 一次收集全部 keypair 名做集合判断，无逐 host 解密
- [TUI 预览未呈现 warning] → Non-goal，stderr 在 CLI 可见即可；TUI 改造留待有真实诉求时

## Open Questions

无。
