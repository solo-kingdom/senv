## Context

复用 `config-encrypt-install` 的 Scope 与计划/执行分层（见 driver D3/D4）。

## Decisions

### D1: 独立计划类型，共享框架
`UninstallItem{Name, Group, TargetPath, Action(delete|noop|changed|error), Reason, Err}` / `UninstallPlan`。与 install 共用 scope 解析、解密与路径展开 helper，但动作语义不同，不强行合并类型。

### D2: changed 的确认粒度
CLI 默认行为：plan 中若含 changed 条目，整体确认后逐条提示 `delete <path> (modified)? [y/N]`；`--yes` 视为全部确认（含 changed），并在 plan 输出中以醒目标注提示。TUI 侧由 `config-encrypt-tui` 实现等价确认。

### D3: 只删安装位置文件
ExecuteUninstall 只 `os.Remove(targetPath)`；目录即使变空也不删除（避免误删用户目录）。

## Risks / Trade-offs
- [`--yes` 一键确认改动文件有误删风险] → plan 输出中 changed 条目醒目标注，文档说明 `--yes` 语义
- [TOCTOU：plan 后文件被改] → Execute 前重新比较，状态升级（noop→changed 等）时跳过并报告

## Migration Plan
无存储变更。
