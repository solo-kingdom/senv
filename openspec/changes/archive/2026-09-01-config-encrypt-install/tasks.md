## 1. 计划框架
- [x] 1.1 定义 Scope / InstallItem / InstallPlan 及 PlanInstall（解密、展开、字节比较、动作判定）
- [x] 1.2 单测：create / skip / backup_overwrite / error（未定义变量）四种动作；scope 三种取值与互斥校验

## 2. 执行
- [x] 2.1 ExecuteInstall：递归 MkdirAll(0755)、备份（.senv-backup-<ts>）、WriteFile(0644)、执行前内容再校验
- [x] 2.2 单测：多级目录创建、备份生成与内容一致、skip 不写盘、错误条目跳过

## 3. CLI
- [x] 3.1 `config install [name] [--group g|--all]`，默认 plan → 确认 → 执行；`--dry-run`、`--yes`
- [x] 3.2 plan 渲染（逐条动作+原因+目标路径，含错误条目标注）
- [x] 3.3 `export` 内部切换到共享执行逻辑，行为回归测试

## 4. 收尾
- [x] 4.1 `make check` 通过
