## Context

现有指针与切换内核只承载单模型：`internal/llm/pointer.go` 的 `AgentPointer` 只有 `Provider`/`Model`/`SwitchedAt`，`SwitchManager.Switch(agentID, providerAlias, model)` 也只接收一个模型。`CONTEXT.md` 的「当前指向」已改为 provider + Agent 模型集 + 默认模型（driver 阶段沉淀），本子 change 让代码与之一致。

## Goals / Non-Goals

**Goals:** 定下指针结构与 version 1 兼容规则；让切换内核接收并校验模型集与默认模型；为 agents 子 change 提供「上一次写入的模型集」

**Non-Goals:** 适配器写回内容、CLI 参数表面、TUI 交互、vault 内任何数据

## Decisions

1. **`version` 仍为 1，新增字段可选**（对应 grill D11）。备选是 bump 到 2 并让旧指针失效——被否：旧记录描述的就是「配置里确实只有这一个模型」，读作单元素集是事实而非猜测，bump 版本只会强迫用户重新切换。
2. **兼容归一放在 `LoadPointers`**：解码后若 `models` 为空且 `model` 非空，则归一为单元素集；写回永远写新结构。这样兼容逻辑只有一处，调用方不分叉。
3. **`SwitchRequest` 用 `Models []string` + `DefaultModel string` 取代 `Model string`**；`Switch` 签名同步。空切片与 nil 都表示「未显式给出」，由调用方在 cli 子 change 里决定默认值（本子 change 由 `cmd` 传单值以免行为提前变化）。
4. **校验顺序**：agent 注册表 → provider 存在 → `api_shape` 兼容 → 模型集非空 → 每个模型属于 Provider 模型集 → 默认模型属于 Agent 模型集 → 解密凭据 → 写回。保持既有「先校验后落盘」，非法输入 MUST NOT 触碰任何文件。
5. **差集依据只提供不消费**：内核只暴露指针里的 `models`，清理决策留给 agents 子 change，避免 store 层知道 agent 配置格式。

## Risks / Trade-offs

- [回滚到旧二进制时指针缺 `model` 字段，旧版本 status 会显示空模型] → 在 Migration Plan 说明：回滚需重跑一次 switch；不为此保留镜像字段
- [模型集较大时指针文件变大] → 指针只存模型 id，且 Provider 模型集本身有上限校验

## Migration Plan

无 vault 迁移。存量指针自动兼容（决策 1/2），无需用户操作。回滚策略：若回到旧二进制并观察到指向显示为空，重跑一次 `senv ai switch` 即可；指针文件本身不需手工编辑。

## Open Questions

无
