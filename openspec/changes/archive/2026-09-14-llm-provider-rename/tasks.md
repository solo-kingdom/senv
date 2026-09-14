## 1. Domain API

- [x] 1.1 在 `internal/llm` 实现 `RenameProvider(old, new)`：校验别名、冲突检测、自有凭据 `RenameText`、写新档案删旧档案、更新 `credential_ref`；按 design D2 做失败逆操作。**验证**：单元测试覆盖成功路径与冲突拒绝。
- [x] 1.2 **[高优先级]** 补齐安全相关用例：外部引用不移动凭据、目标 `llm-keys/<new>` 冲突 fail-closed、自有凭据缺失仍改名、输出/错误不含明文。**验证**：`go test ./internal/llm/ -run Rename`。
- [x] 1.3 实现指针联动（Load/SavePointers，缺失文件记 0）；返回受影响 agent 数。**验证**：有/无指针文件的测试。

## 2. CLI

- [x] 2.1 增加 `senv ai provider rename <old> <new>`，解锁 vault、调用 API、打印指针数与 re-switch 提示；注册 `--help`。**验证**：`go run . ai provider rename --help`；CLI 测试成功/冲突。
- [x] 2.2 CLI 测试与实现配对：旧名不存在、目标冲突、自有凭据联动。**验证**：`go test ./cmd/ -run ProviderRename`（或等价）。

## 3. TUI

- [x] 3.1 AI Tab provider 栏增加 `r`：单字段新别名表单、单选约束、调用同一 rename API、刷新左栏与 toast/审计。**验证**：键位进 `?` 总览；手工或既有 TUI 测试能覆盖入口绑定。
- [x] 3.2 TUI 冲突时表单内联报错且零写入。**验证**：冲突用例或与 1.2 共用 API 断言。

## 4. Docs & 回归

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md` 与 README（rename 语义、edit 仍不可改名、需 re-switch）。**验证**：文档与 `go run . ai provider --help` 一致。
- [x] 4.2 全量相关回归：`go test ./internal/llm/ ./cmd/ -count=1`（至少含 provider/switch 既有套件）。**验证**：通过且无新增失败。
