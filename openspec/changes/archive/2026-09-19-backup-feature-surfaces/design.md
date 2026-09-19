## Context

依赖 `backup-feature-core` 的 Manager。对照 `internal/tui/text_tab.go`、`cmd/mcp.go` 的 text 工具。动机见 proposal.md - Why；切片见 driver design D5。

## Goals / Non-Goals

**Goals:**

- Backup 作为一等 TUI Tab 与 MCP 类型，不塞进 Text Tab
- 搜索找得到，正文不进搜索

**Non-Goals:**

- 同步；CLI 命令组（core 已做）

## Decisions

### D1 独立 Tab，复制 Text 双栏，去掉解引用

键位：`e` vim、`n`/`d`/`r`/`i`/`x`/`+`、过滤 `/`、多选批量 `d`/`x`。不提供 `D`（无引用）。`default` 不可改名/删除。备选「Text Tab 过滤器」否——与产品隔离冲突。

### D2 全局搜索扩类型 Backup

匹配 group/key/description，结果标注类型并可跳转。不匹配 value。

### D3 MCP 对齐 text 四件套

无 `decode` 参数。`senv_group_add` description 由 env|text 扩为 env|text|backup。

### D4 SKILL.md 分段

本切片只改 TUI 键位与 MCP 工具清单；CLI 段属 core，同步段属 sync。避免无意义整文件重写，但仍可能与 sync 碰同一文件 → 串行 apply。

## 数据流

```
TUI Backup Tab ── backup.Manager ── backups/
MCP senv_backup_* ── 同上（需 session）
S 搜索 ── 快照标识字段（含 backup group/key/description）
```

## 错误处理策略

- 缺组/超限：表单或 toast 显示 Manager 错误，不写盘
- MCP 无 session：沿用既有授权失败
- 搜索无匹配：空列表，不报错

## 向后兼容

新 Tab 出现在注册序列中；旧快捷键 `1`–`9` 可能后移，以实际注册顺序为准（tui-viewer 既有约定）。

## Risks / Trade-offs

- [Tab 序号变化] → `?` 键位总览与底栏随注册顺序生成，不写死数字
- [与 sync 同时改 SKILL.md] → driver 规定串行

## Open Questions

无。
