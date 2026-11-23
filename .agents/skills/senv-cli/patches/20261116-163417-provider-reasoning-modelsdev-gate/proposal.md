# provider 操作加「models.dev 先行 + 档位声明=网关实测」门禁,防 pi 思考档只剩 off

- target: .agents/skills/senv-cli
- mode: update
- patch: 20261116-163417-provider-reasoning-modelsdev-gate
- risk: medium
- status: proposed

## Intent

把 2026-11-16 open-design→pi 排障结论沉淀进 senv-cli skill 的 LLM Provider 操作约定:

1. 新增一条操作约定 bullet:操作 provider(新增/编辑模型集、补元数据)前先查 models.dev 核对 `limit`/`modalities`/`reasoning_options`;声明推理档位(`--model-reasoning`)前必须对该 provider 网关实测(目录≠网关);不声明推理档位的模型切 pi 后思考档只剩 off;想让 pi 保留 off 档,档位列表必须显式含 `none`;补声明后须重跑各机 `switch` 重投影。附经验库指针。
2. 修正「模型元数据随切换投影」bullet:pi 除 `reasoning: true` 外还写 `thinkingLevelMap`(七档固定键,未声明档写 `null`,pi 据此隐藏该档)——文档此前缺该字段,与代码实际行为不同步。

非目标:不改 senv 本身行为、不动其他章节、不夹带润色。

## Conflict check

- 与「新增或替换模型集时必须校验 context window」bullet 互补不冲突(context 已强制走 models.dev,本 patch 把 efforts 目录查询也纳入并强调"目录仅供参考");与「模型元数据随切换投影」bullet 是同一主题的字段补全。
- 与其他 skill 职责无交集(exp-ledger 经验条目是数据源指针,不重复其正文)。

## Rationale

今天排障实证:档案未声明推理档 → senv 投影不写 `reasoning`/`thinkingLevelMap` → pi 恒只给 off;且 models.dev `reasoning_options` 与真实网关可不一致(gpt-6-luna 目录有 max、网关 400)。skill 维护约定本身要求「子命令 help 无法覆盖的行为以实现、测试和 ADR 为准;有变化时回写本指南」,thinkingLevelMap 属文档滞后,一并回写。改动可验证:patch 应用后 grep 两个锚点即可确认。

## Files

- `.agents/skills/senv-cli/SKILL.md` — LLM Provider 与 coding agent 一节:新增 1 条 bullet(行 118 之后)+ 投影 bullet 补 `thinkingLevelMap` 字段说明。

## Validation

- 应用前:`git apply --check --recount` 通过。
- 应用后:`git diff --check` 无空白错误;grep 确认「推理档位声明=网关实测」与「thinkingLevelMap」两锚点存在;frontmatter 未动;无隐私信息(无内网 URL/凭据,open-design 为公开服务名)。
