## 1. 存储与目录解析

- [x] 1.1 `LLMModelInfo` 增加 `DefaultReasoning`、`InputModalities`（omitempty）；旧档案无字段仍可 Load。验证：`go test ./internal/storage -run LLMProvider`
- [x] 1.2 `catalogModelEntry` / `ModelMetadata` 读取 `modalities.input`（及若存在的 default effort）；缺字段为零值。验证：`go test ./internal/llm -run LoadModelMetadata`

## 2. 装配校验

- [x] 2.1 `assembleModels`：显式 per-model > 档案已有 > 集合级默认档 > 目录；集合级只填充有档位且尚未解析出默认档的模型；有档位缺默认档拒绝；默认档必须属于档位列表；无档位不要求。验证：`go test ./internal/llm -run Assemble`
- [x] 2.2 配套测试：集合级填充、无档位放过、默认档越界、模态吸入/显式、不从列表首项推断。验证：同上包测试全绿

## 3. CLI

- [x] 3.1 `senv ai provider add/edit` 增加 `--model-default-reasoning`、`--default-reasoning`、`--model-modalities`；改动档位时走与 add 相同的必填校验。验证：`go test ./cmd -run AIProvider`
- [x] 3.2 `show` 输出默认推理档与输入模态；MCP `model_info` 透传新字段。验证：cmd 单测 + `go test ./cmd -run MCP` 或现有 llm MCP 测试
- [x] 3.3 配套测试：有档位缺默认档非 0、集合级成功、edit 补默认档、show 含新字段。验证：`go test ./cmd -run AIProvider`

## 4. 切换投影

- [x] 4.1 Codex：声明的默认档写入 `default_reasoning_level`；缺省或无档位写单档 `none`（不取列表首项）；缺模态写 `["text"]`，有则写档案值。验证：`go test ./internal/llm -run Codex`
- [x] 4.2 Kimi / Pi / OpenCode：模态含 image/video 时写对应能力；缺席省略。验证：`go test ./internal/llm -run 'Kimi|Pi|OpenCode|projection'`
- [x] 4.3 旧档案有档位无默认档：切换成功、不回写 vault、提示可 edit。验证：switch 单测断言 catalog 为 `none` 且档案未变

## 5. TUI

- [x] 5.1 AI Tab 表单增加默认推理档与输入模态字段；编辑预填；有档位缺默认档内联报错。验证：`go test ./internal/tui -run AIProvider`
- [x] 5.2 详情弹层展示默认推理档与输入模态。验证：同上或详情相关测试

## 6. 文档与收尾

- [x] 6.1 更新 `.agents/skills/senv-cli/SKILL.md` 的 provider add/edit/show 与 TUI 说明。验证：`go run . ai provider add --help` / `edit --help` 与文档一致
- [x] 6.2 `make check` 全绿；`openspec validate enrich-model-metadata --strict` 通过
