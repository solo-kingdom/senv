## Context

见 `proposal.md`。现状：`LLMModelInfo` 含 name / description / context_window / output_limit / reasoning_efforts；装配只强制 context window；Codex 投影把 `default_reasoning_level` 取成档位列表首项，且不写 `input_modalities`。模型目录缓存已透传 models.dev JSON，但 `LoadModelMetadata` 未读 `modalities`。ADR-0014：默认推理档是声明值，不按模型推断。

## Goals / Non-Goals

**Goals:**
- 档案 schema 增加默认推理档与输入模态，装配/投影/展示走同一套解析优先级
- 有档位则默认推理档必填；无档位不要求；旧档案缺字段可切换、不回写
- Codex / Kimi / Pi / OpenCode 按声明投影，缺省只用 agent 模板（`none` / `["text"]`）

**Non-Goals:**
- 不把 Codex plumbing 或 cost/knowledge 写入档案
- 不在 senv 内维护 per-model 特例表
- 不改 zcode/cursor

## Decisions

1. **解析优先级**（与 context window 同构，多一层集合级）：显式 per-model > 档案已有 > 集合级 `--default-reasoning` > 模型目录。集合级只填充「有档位且尚未解析出默认档」的模型。
   - 备选：无集合级、每个模型都写 flag → 目录 add 在 models.dev 补字段前不可用。
2. **存储**：`LLMModelInfo` 增 `DefaultReasoning string`、`InputModalities []string`；未知用 omitempty，不写空数组冒充「纯文本」。
3. **目录**：`catalogModelEntry` 增 `modalities.input`；default effort 目录没有就不填，等上游出现再吸入，装配逻辑不变。
4. **Codex 投影模板**：无档位或旧档案缺默认档 → 单档 `none`（不取列表首项）；缺模态 → `["text"]`。有声明则写真实值。有档位时 `supports_reasoning_summaries=true` 仍作 plumbing，不落盘。
5. **Kimi/Pi/OpenCode**：模态含 image/video 才加对应能力字段；缺席省略，不写 false。

## 数据流

```
CLI/TUI 显式值 ─┐
档案既有元数据 ─┼─▶ assembleModels ─▶ LLMModelInfo 落 vault
集合级默认档  ─┤         │
模型目录缓存  ─┘         │ 缺必填 → 拒绝写入
                         ▼
                   senv ai switch
                         │
            ┌────────────┼────────────┐
            ▼            ▼            ▼
         Codex        Kimi        Pi/OpenCode
      catalog 模板    capabilities   input / modalities
```

## 错误处理策略

- add/edit：缺 context、有档位缺默认档、默认档不在列表、模态/模型名非法 → 非 0，不写档案或凭据
- TUI：同一校验经提示条回显，留在表单
- switch：旧档案缺默认档/模态不失败；Codex 用模板；stderr/成功输出提示可 edit 补声明
- 目录缺字段：不当成错误，只是该层解析为空

## 向后兼容

- 旧档案 JSON 无新字段仍可 Load；edit 其它字段不因缺默认档失败
- 新 add 对「有档位的目录模型」更严：必须带 `--default-reasoning` 或 per-model 声明
- 不迁移、不回写旧 vault

## CLI 示例

```bash
senv ai provider add minimax --catalog-provider minimax \
  --default-reasoning high --api-shape openai-responses

senv ai provider add local --base-url https://api.example.com/v1 \
  --model custom-1 --model-context custom-1=1000000 \
  --model-reasoning custom-1=low;high \
  --model-default-reasoning custom-1=high \
  --model-modalities custom-1=text,image

senv ai provider edit main --model-default-reasoning m1=high
senv ai switch codex minimax
```

## Risks / Trade-offs

- [models.dev 长期无 default effort] → 集合级 `--default-reasoning` 兜底；目录一旦有字段自动吸入
- [旧档案切 Codex 落到 Think-Off] → 输出提示 edit；不静默猜 `high`
- [TUI 表单多两行] → 详情展示全量；编辑只加默认档与模态，不加 cost 等

## Open Questions

无
