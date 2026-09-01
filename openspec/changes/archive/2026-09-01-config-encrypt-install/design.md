## Context

见 driver D3/D4。`internal/config.Manager.Export` 已实现"解密 + MkdirAll + 写文件"的骨架，但无计划/校验/备份，且 scope 仅单条。本 change 在其上抽象 Plan/Execute。

## Decisions

### D1: 计划为纯数据结构
```go
type InstallItem struct {
    Name, Group, TargetPath string
    Action string // create | skip | backup_overwrite | error
    Reason string // 人类可读说明
    Err    error
}
type InstallPlan struct{ Items []InstallItem }
```
`PlanInstall(scope Scope) (*InstallPlan, error)` 只读（解密 + 展开 + 比较），`ExecuteInstall(plan)` 幂等执行。CLI 渲染与确认在 cmd 层。

### D2: scope 统一解析
`type Scope struct{ Name, Group string; All bool }`，CLI flag 映射；解析阶段校验互斥（name 与 --group/--all 不同时给）与存在性。

### D3: 复用 Execute 单条逻辑
单条执行 = 解密 → MkdirAll(0755) → （如需）备份 → WriteFile(0644)。导出旧命令 `export` 保留为兼容入口，内部改走同一执行逻辑但维持原交互（无 plan），避免破坏既有用法。

## Risks / Trade-offs
- [备份文件堆积] → 命名含时间戳便于识别，文档注明自行清理；不做自动保留策略
- [plan 到 execute 之间目标文件被第三方改动] → TOCTOU 窗口存在但本机 CLI 场景可接受；Execute 前重新校验一次内容，若与 plan 假设不符则按新状态处理
- [大量配置时逐条解密慢] → 文件小、数量少，可接受

## Migration Plan
无存储变更；`export` 行为保持兼容。
