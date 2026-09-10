## Context

grill 已收敛：`grill.md` 的 D1–D11 全部 settled、无未决问题，术语已沉淀至 `CONTEXT.md`（Provider 模型集 / Agent 模型集 / 默认模型 / 漂移）。现状是切换只写单个模型：`internal/llm/switch.go` 各 adapter 分别写 claude-code 的 `model`、codex 的 `model`、kimi 的单条 `[models.*]`、pi 的单元素 `models[]`、opencode 的单个 `models` 键；指针 `AgentPointer` 只记 `(provider, model)`。driver 无代码变更，只编排 4 个子 change；实现细节下沉到各子 change 的 `design.md`。

## Goals / Non-Goals

**Goals:** 按功能闭环切成 4 个可独立 apply/validate 的子 change；固定实施顺序；保证跨子 change 依赖被顺序满足

**Non-Goals:** 任何实现层决策（见各子 change design）；修改 driver 协议本身；provider 档案 schema、凭据引用与接入地址归一规则（ADR-0004/0006 不变）

## Decisions

1. **按层切片**（对应 grill 决策编号）：
   - `agent-model-set-store`：指针承载 Agent 模型集与默认模型、v1 旧指针兼容、切换内核入参与校验、清理所需差集依据（D4/D11）
   - `agent-model-set-agents`：五个 agent 的模型集投影（claude-code `modelPicker`、codex catalog 生成与元数据合成、kimi 多条 `[models.*]`、pi `models[]`、opencode `models{}`）、旧痕迹清理与事务扩展（D1/D3/D5/D6/D7/D10）
   - `agent-model-set-cli`：命令参数表面（`--models`/`--default-model`、`--model` 报错）、`status` 条数与漂移、审计 detail、`.agents/skills/senv-cli/SKILL.md` 同步（D2/D3/D8/D9）
   - `agent-model-set-tui`：AI Tab 模型集多选与「仅换默认模型」语义（D1/D9）
2. **实施顺序**：store → agents → cli → tui。store 先定状态形状；agents 的写回依赖它；cli 暴露该语义；tui 复用 SwitchManager，与 cli 相互独立。
3. **capability 全部复用既有**：modified `llm-provider-switch`（切换语义、指针存储、status、事务）与 `llm-provider-tui`（Tab 内切换操作），不新增 capability。
4. **跨子 change 约束**（已晋升 ADR，归档时确认状态）：写入只在 `senv-<alias>` 命名空间与本次必须改的键，切换时清理自己上一次的痕迹（ADR-0012）；codex 走生成的 catalog 文件并合成必填元数据（ADR-0011）；切换写入仍是「先校验后落盘、失败不留半写」。
5. **CLI 使用示例**（跨子 change 统一口径，各子 change design 细化）：
   ```bash
   senv ai switch claude-code myprovider                      # 默认全选 Provider 模型集
   senv ai switch claude-code myprovider --models m1,m2 --default-model m1
   senv ai switch codex myprovider --default-model m2         # 已指向该 provider：仅换默认模型
   senv ai status
   ```

## 数据流（子 change 依赖）

```
store ──▶ agents ──┬──▶ cli
                   └──▶ tui
```

## 错误处理策略

driver 层无运行时错误面；跨子 change 只约束语义：切换写入失败不得留下半写状态（含本次新建的 catalog 文件与删除动作，由 agents 落实）；参数非法不改任何文件（由 cli 落实）；指针只在配置写回成功后更新，失败回滚配置（store 保持既有事务语义）。

## Risks / Trade-offs

- [codex `model_catalog_json` 是未公开 schema，codex 升级可能改必填集] → agents 子 change 以本机 codex 做解析回环，写入前自校验，失败报告真实错误
- [清理动作可能误删用户内容] → 只按 `senv-<alias>` 前缀匹配并纳入事务，agents 子 change 用测试覆盖
- [kimi 元数据依赖目录缓存新鲜度] → 缺失时回退保守值，不因目录缺失而失败
- [4 个子 change 同仓串行 apply，中途状态不一致] → 每个子 change 完成即 `validate --strict` 且保持可构建，driver 对应 checkbox 才勾选

## Migration Plan

指针 v1 兼容（旧指针读作单元素集，无需用户重新切换，由 store 落实）；无 vault 数据迁移。实施全部完成后按协议归档：先归档全部子 change（spec 应用到 `openspec/specs/`），再归档 driver。

## Open Questions

无
