# ADR：backup v1 范围

- 状态：已冻结（decide）
- 日期：2026-09-18

## 决策

- MCP 对齐 text：`list` 无 value；`get/set/delete` 有正文；分组 `kind=backup`；无保留组封锁。
- `senv init` 预置 backup 的 `default` 组，不另建预置组。
- TUI 全局搜索 `S` 包含 backup 的 group/key/description，不搜正文。
- v1 非目标：TTL/缓存、引用、改根快捷、text→backup 自动搬家、目录级备份、>512KB、CLI rename。
- 512KB 只计 value 明文；description/信封不计；超限拒绝、不截断。
- 分组与说明照搬 text：description 最多 2048 字节；`group add` 说明必填；禁止隐式建组；TUI 中 `default` 不可改名/删除。
- `set`/`import` 直接覆盖（upsert），刷新 `updated_at`，无确认。
- 做成标准：CLI `backup` 命令组 + TUI Backup Tab + MCP 四件套 + git/server 同步新 kind + 更新 senv-cli skill；超限与隐式建组有测试。交付时补与 text-storage 对等的 `backup-storage` spec。

## 主因

与 text 的交互契约保持可预期；冷数据靠独立 Tab/kind 隔离，而不是砍 agent 能力或藏搜索。超限截断会悄悄丢备份；backup 不为冷数据另开隐式建组或覆盖确认，避免两套心智。
