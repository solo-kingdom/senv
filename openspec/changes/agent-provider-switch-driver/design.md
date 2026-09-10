## Context

grill 已收敛：`grill.md` D1–D11 全部 settled、无未决问题，术语已沉淀至仓库根 `CONTEXT.md`（LLM Provider / Coding Agent / 模型目录 / 切换 / 当前指向）。driver 无代码变更，只编排 5 个子 change；本设计只定切片、顺序与依赖，实现细节下沉到各子 change 的 design.md。

## Goals / Non-Goals

**Goals:** 按功能闭环切成 5 个可独立 apply/validate 的子 change；固定实施顺序；保证跨子 change 依赖被顺序满足
**Non-Goals:** 任何实现层决策（见各子 change design）；修改 driver 协议

## Decisions

1. **按功能闭环切片**（对应 grill 决策编号）：
   - `agent-provider-switch-catalog`：模型目录缓存 + `senv ai refresh`（D5），并建立 `senv ai` 根命令（D6）
   - `agent-provider-switch-store`：LLM Provider 档案 vault 存储 + `senv ai provider add/list/show/remove`，添加时自动从目录加载模型、支持自定义模型（D1/D4/D5）
   - `agent-provider-switch-agents`：coding agent 注册表（claude-code、codex、zcode、kimi、pi、opencode；cursor best-effort）+ 各 agent 配置适配器写回 + `senv ai switch/status`（D2/D3/D7/D8/D11）
   - `agent-provider-switch-tui`：TUI 浏览 provider 与当前指向 + 切换操作（D9/D10）
   - `agent-provider-switch-mcp`：MCP 只读查询（provider 列表、当前指向）（D9/D10）
2. **实施顺序**：catalog → store → agents → tui → mcp。store 依赖 catalog（add 填充模型集）；agents 依赖 store（档案与凭据解密）；tui、mcp 依赖 store + agents，二者相互独立，排在最后。
3. **capability 均为新增**：`llm-model-catalog` / `llm-provider` / `llm-provider-switch` / `llm-provider-tui` / `llm-provider-mcp`，不修改既有 requirement（`provider-abstraction` 是同步后端能力，不触碰）。
4. **跨子 change 约束**（ADR 候选，归档时晋升 `docs/adr/`）：
   - 切换后凭据明文落在 agent 配置中是接受的妥协（agent 无法自行从 vault 拉取）；写入文件权限收敛
   - Coding Agent 当前指向是本机状态，不进 vault 不同步；档案进 vault 同步
5. **CLI 使用示例**（跨子 change 统一口径，各子 change design 细化）：
   ```bash
   senv ai refresh
   senv ai provider add myprovider --base-url https://... --key-ref env/openai/key
   senv ai switch claude-code myprovider --model claude-sonnet-4
   senv ai status
   ```

## 数据流（子 change 依赖）

```
catalog ──▶ store ──▶ agents ──┬──▶ tui
                    └──────────┴──▶ mcp
```

## 错误处理策略

driver 层无运行时错误面；跨子 change 只约束语义：切换写入失败不得留下半写状态（适配器先备份/原子写，由 agents 子 change 落实）；目录拉取失败时回退旧缓存并明确报错（catalog 子 change 落实）。

## Risks / Trade-offs

- [各 agent 配置格式异构，opencode/pi 等的配置事实未逐个核实] → agents 子 change 先做配置格式勘察，适配器以真实配置文件验证；cursor 无法覆盖时按 D2 降级为不支持并在 status 明示
- [切换写入明文 key 到 agent 配置] → 遵循决策 4 约束，文件权限收敛，不做加密代理
- [5 个子 change 同仓串行 apply，中途状态不一致] → 每个 apply 完成即 `validate --strict` 并保持可构建；driver 对应 checkbox 才勾选
- [models.dev 目录结构变化] → catalog 缓存带格式版本与校验，解析失败保留旧缓存并报错

## Migration Plan

新功能，无存量数据迁移。实施全部完成后按协议归档：先归档全部子 change（spec 应用到 `openspec/specs/`），再归档 driver。

## Open Questions

无
