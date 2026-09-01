## Why
安装的逆操作缺失：用户需要把配置从保存位置移除。目标文件若被本地改动过，直接删除会丢数据，必须先提示确认；未改动则可直接删除。与 install 一样需要先 plan（dry-run）再确认。

## What Changes
- `internal/config` 复用 plan 框架新增 `PlanUninstall(scope)` / `ExecuteUninstall(plan)`，scope 支持单条 / 分组 / 全部
- uninstall 行为：目标不存在 → 视为已完成（noop）；内容与解密内容相同 → delete；不同 → 标记 changed，执行前需显式确认
- CLI：`config uninstall [name|--group g|--all]`，默认 plan → 确认；含 changed 条目时需逐条或整体二次确认；`--dry-run`、`--yes`

## Non-goals
- 不删除加密存储中的配置本体（仅删除安装位置文件；删除存储本体仍走 `config delete`）
- 不做回收站/可恢复删除
- 不改 TUI（属 `config-encrypt-tui`）

## Capabilities
- `config-uninstall`: 配置卸载（plan/confirm、改动检测与确认）

## 依赖
- `config-encrypt-install`：plan 框架与 scope 解析

## 验收标准
- [ ] uninstall 先输出 plan，确认后执行
- [ ] 目标文件与存储内容一致时直接删除
- [ ] 目标文件被改动时 plan 标注，需显式确认才删除
