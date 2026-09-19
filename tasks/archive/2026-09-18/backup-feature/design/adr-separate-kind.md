# ADR：backup 作为独立持久 KV，不复用 text

- 状态：已冻结（decide）
- 日期：2026-09-18

## 决策

- 新增独立 vault kind `backup`，不与 text 共用存储或分组。
- 语义是持久备份，不是缓存（无 TTL、非本机-only）。
- value 为调用方塞入的不透明字节（不校验 UTF-8）；二进制走文件进出。
- v1 交互面：CLI `senv backup …`、独立 TUI Backup Tab、MCP；根快捷 `senv <group:key>` 仍只写 text。
- v1 不参与 `{{…}}` 引用。

## 主因

text 常用、backup 不常用，混在同一列表/Tab 体验差。引用会把冷数据拉回 env export / decode 热路径。

## 后果

- 存储、分组、TUI、MCP 都按新 kind 铺一套，而不是 text 的别名。
- 取用备份用 get / export，不用模板展开。
