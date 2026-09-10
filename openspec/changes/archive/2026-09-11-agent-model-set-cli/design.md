## Context

`cmd/ai_switch.go` 现在只暴露 `--model`，`senv ai status` 输出 `provider / model（时间）`；审计 detail 为 `provider:<alias> model:<model>`。switch 子 change 已让 `SwitchManager` 接收模型集与默认模型，agents 子 change 已实现写回与清理。

## Goals / Non-Goals

**Goals:** 让参数表面与状态展示表达模型集语义；`--model` 的旧用法明确失败；输出与审计反映模型数

**Non-Goals:** 写回实现、清理算法、TUI、指针结构、provider 档案命令

## Decisions

1. **`--model` 直接报错而非静默别名**（grill D8）。旧用法是「只写这一个模型」，新语义是「选定集 + 默认」；静默把它当作 `--models m --default-model m` 会让旧脚本在无提示下改变写入结果。
2. **参数校验前置于 vault 解锁**：`--model` 与 flag 形式错误在 cobra 层即拒绝，不做解锁与解密；模型集成员校验需要档案，发生在解锁之后、任何写文件之前。
3. **`--default-model` 不回写档案**（grill D2）：它只影响本次写入的默认模型；档案默认模型的修改仍走 `senv ai provider edit`。
4. **status 漂移判据 = 指针模型集 vs 档案模型集**（grill D4/D9），不解析 agent 配置；档案缺失时按「档案不可用」提示，不回退为「未切换」。
5. **审计 detail 用 `default:<model> models:<count>`**：沿用既有审计不加值的原则，模型名与计数不是凭据材料。

## 数据流

```
argv ──▶ cobra flag 校验（--model 拒绝、--models 解析）
      ──▶ 解锁 vault ──▶ 读档案 ──▶ 模型集/默认模型校验 ──▶ SwitchManager.Switch(models[], defaultModel)
      ──▶ 输出：provider + 条数 + 默认模型 + 实际接入地址        审计：default:X models:N

senv ai status ──▶ 指针（免解锁）+ 档案（如需漂移判定，需解锁）──▶ provider / 默认模型（N 个模型）+ 漂移
```

## 错误处理策略

- `--model` 存在 → 立即非 0 退出并给出替代用法，不解锁、不写文件
- `--models` 值不在档案模型集、显式空集、档案无默认且未指定 → 非 0 退出并列出可用模型，不写任何文件
- 指针模型集与档案不一致只影响提示，不阻塞 status 输出

## Risks / Trade-offs

- [status 的漂移判定需要解锁 vault 才能读档案] → 指针读取仍免解锁；档案不可得时只省略漂移判定并保持既有输出，绝不因此报错
- [移除 `--model` 破坏既有脚本] → 属有意 breaking（D8），错误信息给出可直接替换的两种写法；同步更新 skill 文档

## Migration Plan

无数据迁移。既有脚本若使用 `--model` 会立即失败并得到替代提示，按提示改写即可。

## Open Questions

无
