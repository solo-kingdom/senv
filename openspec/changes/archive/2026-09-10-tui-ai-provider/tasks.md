## 1. 存储与后端 [高优先级]

- [x] 1.1 `LLMProviderEntry` 增加 `api_shape` 字段与校验（枚举内取值，空值合法）；补单测覆盖缺省、非法值、序列化向后兼容。验证：`go test ./internal/storage`。
- [x] 1.2 `ProviderManager` 新增 `EditProvider`：可改 base_url/模型集/default_model/目录来源/凭据引用，alias 不可改；凭据轮换语义与 `AddProvider` 一致（新凭据覆盖、改外部引用删自有、未提供则保留）。验证：`go test ./internal/llm -run EditProvider`。
- [x] 1.3 归一与切换接入 `api_shape`：空值走既有 `baseURLForFamily`，非空以档案为准；`Switch` 在写回前校验与目标 agent 协议族的兼容性，不兼容时拒绝且不写文件。验证：`go test ./internal/llm -run 'Switch|BaseURL'`。
- [x] 1.4 `cmd/ai_provider.go` 新增 `edit` 子命令与 `--api-shape`（add/edit 通用），输出实际生效的接入形态与归一后接入地址。验证：`go test ./cmd -run Provider` 与 `go run . ai provider edit --help`。

## 2. AI Tab 布局与焦点 [高优先级]

- [x] 2.1 重写 AI Tab 为两栏：左 Providers（含指向标记）、右 Agents（`agent · provider/model · 未切换/不支持`）；`←→/hl` 切换焦点并用高亮指示，`↑↓` 只作用于当前焦点栏。验证：`go test ./internal/tui -run AI`。
- [x] 2.2 provider 详情移入 `enter` 弹层（base_url、模型集、凭据引用、api_shape、目录来源），列表行按截断规则处理，长模型集不再折行。验证：`go test ./internal/tui -run AI`。

## 3. AI Tab 写操作 [高优先级]

- [x] 3.1 provider 新建与编辑表单（别名、base_url、api_shape、目录来源、模型集、默认模型）；编辑时别名只读；提交调用 `AddProvider`/`EditProvider`。验证：`go test ./internal/tui -run AI`。
- [x] 3.2 凭据录入：默认列出既有 env/text 条目供选择（`--key-ref` 语义），另提供遮蔽输入新建自有凭据；明文不进 TUI 状态与渲染文本。验证：`go test ./internal/tui -run AI` 与人工全界面扫描。
- [x] 3.3 provider 删除（`d` + 确认，沿用自有凭据处理语义）与操作审计（`AuditOpLLMProvider`）。验证：`go test ./internal/tui -run AI` 与 `senv audit` 实查。
- [x] 3.4 `s` 切换改为对右栏选中 agent 生效；新增 `m` 仅换模型（同 provider，新 model），失败经统一提示条回显且指针不变。验证：`go test ./internal/tui -run AI`。

## 4. 文档与整体验证

- [x] 4.1 更新 `README.md`（AI Tab 键位与 `api_shape` 说明）、`.agents/skills/senv-cli/SKILL.md`（provider edit、api-shape、TUI 编辑）。验证：人工比对 `go run . ai provider edit --help` 与文档。
- [x] 4.2 运行 `make check`，修复 fmt/vet/lint/race 问题。验证：`make check` 全部通过。
