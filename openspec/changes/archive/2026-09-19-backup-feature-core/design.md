## Context

对照 `internal/text/manager.go`、`internal/storage` 的 text 段、`cmd/text.go`。动机见 proposal.md - Why；切片边界见 driver `../backup-feature-driver/design.md`（D1–D3、D6–D7）。

## Goals / Non-Goals

**Goals:**

- 可独立使用的 backup 存储 + CLI，行为与 text 对齐（除引用与根快捷）
- 新 vault 与存量 vault 都能往 `default` 写入

**Non-Goals:**

- TUI、MCP、同步通道（其它切片）

## Decisions

### D1 复制 text 模块边界，不抽共享泛型

新增 `internal/backup` 与 storage 的 backup 段，而不是把 text 改成泛型 KV。备选「text 加 type 字段」否——混用体验正是要避免的。允许两套相近代码，换独立演进。

### D2 `MaxBackupSize` 独立常量

`internal/storage` 增加 `MaxBackupSize = 512 * 1024`，与 `MaxTextSize` 同值不同名。

### D3 存量 `default`

Manager 打开时若 backup `default` 组不存在则创建（内置说明，例如 `Default backup group`），幂等。不创建其它组。

### D4 CLI 无 decode

`backup get` 对齐 text 的 `-o/--copy/--file` 与 export `--path`，不提供 `-d/--loose`。

## 数据流

```
senv backup set/import → 校验组存在、大小、说明 → AES-256-GCM → backups/{g}/{k}.enc
senv backup get/export → 解密 → stdout 或 0600 文件（不解析引用）
senv backup group add → 必填说明 → .meta.enc
```

## 错误处理策略

- 超限、缺组、非法名、说明超长：非 0 退出，零写入
- import 源文件不存在：报错、不写 vault
- group delete：交互确认 `[y/N]`

## 向后兼容

只新增 `backups/`。不改 texts/envs。旧二进制忽略该目录。

## Risks / Trade-offs

- [与 text 代码漂移] → CLI/测试对照 text 清单逐项打勾
- [存量 vault 静默建 default] → 仅 `default`，带固定说明，可在 doctor 中看见

## Open Questions

无。
