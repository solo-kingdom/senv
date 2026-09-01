## Why
现有 `config export` 直接覆盖写出，无变更校验、无备份、无预览。把私密配置安装到对应位置是高频高风险操作，需要先给 plan（dry-run）、确认后执行，且目标文件有改动时必须先备份。

## What Changes
- `internal/config` 新增统一的操作计划框架：`PlanInstall(scope)` → 渲染确认 → `Execute(plan)`；scope 支持单条配置 / 单分组 / 全部
- install 行为：目标不存在 → create；内容相同 → skip；内容不同 → 备份后覆盖；父目录不存在 → 递归创建
- 备份命名 `<target>.senv-backup-<yyyymmddHHMMSS>`，与目标同目录
- CLI：`config install [name|--group g|--all]`，默认先输出 plan 并交互确认，`--dry-run` 只输出 plan，`--yes` 跳过确认

## Non-goals
- 不实现 uninstall（属 `config-encrypt-uninstall`）
- 不改 TUI（属 `config-encrypt-tui`）
- 备份不做自动清理与保留策略

## Capabilities
- `config-install`: 配置安装（plan/confirm/execute、校验、备份、递归建目录）

## 依赖
- `config-encrypt-storage`：分组模型与 `ResolveTargetPath`

## 验收标准
- [ ] install 单条/分组/全部均先输出 plan，确认后才执行
- [ ] 目标内容有变化时先生成备份再覆盖
- [ ] 父目录缺失时递归创建
