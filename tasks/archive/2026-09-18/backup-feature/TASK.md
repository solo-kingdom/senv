# Backup 功能

- slug: backup-feature
- status: archived
- created: 2026-09-18
- updated: 2026-09-18
- archived: 2026-09-18
- handed-off: 2026-09-18
- driver: backup-feature-driver

## 目标

- 新增独立 vault kind `backup`：备份不常用数据，不与常用 text 混在同一体验里
- 字段对齐 text：group + key + value + description，AES-256-GCM 加密
- 单条 value 明文上限 512KB，超限拒绝
- v1 同时落地 CLI `senv backup …`、TUI Backup Tab、MCP；并随 git/server 同步

## 非目标

- TTL / 本机缓存 / 自动驱逐（「缓存」作废，只做持久备份）
- `{{backup:…}}` 以及 backup 内容内的 env/text 引用解析
- 改根快捷 `senv <group:key>`（仍只写 text）
- 从 text 自动搬家、目录级备份、超过 512KB
- CLI rename（改名只走 TUI，与 text 相同）

## 现状

text 已是加密文本块：`texts/{group}/{key}.enc`，value 上限 512KB，CLI/TUI/MCP 齐全。backup 要平行做一套，不复用 text 存储或分组。

对照与约束见：

- 术语：`glossary.md`
- `design/adr-separate-kind.md` — 独立 kind、持久、不透明字节、无引用
- `design/adr-v1-scope.md` — MCP/init/搜索/限制与做成标准

text 关键路径：`cmd/text.go`、`internal/text/manager.go`、`internal/tui/text_tab.go`、`openspec/specs/text-storage/spec.md`。

## 进展

- 2026-09-18：立项
- 2026-09-18：explore 完成产品决策（独立 kind、对齐 text 的命令/规则、隔离引用与 Tab）
- 2026-09-18：decide 冻结采纳方案

## 决策

- 采纳：**独立 backup kind + 对齐 text 的 v1 契约** — 主因：text 常用、backup 不常用，混用体验差；其余行为跟 text 走，避免两套心智
- 取舍：
  - 接受：平行存储/分组/TUI Tab/MCP/同步成本；`set`/`import` upsert 无确认；init 预置 backup `default`
  - 放弃：复用 text 或约定组名、缓存/TTL、`{{backup:…}}`、抢根快捷、覆盖确认、隐式建组、CLI rename、目录级备份、>512KB、text→backup 自动搬家
- 带进实现的未决：无。未列出的产品分叉视为本探索任务内已关闭。实现对照 text 铺存储、同步 schema、命令、Tab、MCP、senv-cli skill，并补 `backup-storage` spec
- 回退：若独立 kind 过重，改为 text 分组约定即可，无需数据迁移协议；代价是回到混用体验。已写入的 `backups/` 树需另行清理或导入 text

方案全文：`design/adr-separate-kind.md`、`design/adr-v1-scope.md`。

## 交接

- driver: `backup-feature-driver`
- 采纳：独立 backup kind + 对齐 text 的 v1 契约（见决策）
- 方案：`design/adr-separate-kind.md`、`design/adr-v1-scope.md`
- 带进实现的未决：无
- 归档路径：`tasks/archive/2026-09-18/backup-feature/`

## 未决问题

- [x] backup 与 text 的边界 — 独立 kind；不是缓存
- [x] CLI 与 TUI 是否必须同时落地 — 是，外加 MCP
- [x] 512KB 限制范围 — 只计 value 明文，超限拒绝

## 下一步

- 已交接：交付进度只认 `backup-feature-driver` 的 OpenSpec checkbox
