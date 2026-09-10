## 1. 存储层能力 [高优先级]

- [x] 1.1 `env.Manager` 新增 `RenameKey`、`RenameGroup`、`DeleteGroup`：组内 rename 保持原子（`mutate`），default 分组拒绝重命名/删除，删除激活分组需调用方显式确认；补单测覆盖重名、缺失 key/group、激活组删除。验证：`go test ./internal/env`。
- [x] 1.2 `text.Manager` 新增 `RenameKey`、`RenameGroup`：与既有 `DeleteGroup` 保持同一套校验与加锁；补单测。验证：`go test ./internal/text`。
- [x] 1.3 `config.Manager` 新增 `Rename`（改 name），并保留既有 `SetMeta` 语义用于分组/描述变更；补单测覆盖重名冲突与元信息写入。验证：`go test ./internal/config`。

## 2. 表单组件 [高优先级]

- [x] 2.1 新增 `internal/tui/form.go`：字段列表模型（文本/遮蔽/枚举选择/引用选择/路径）、`Tab`/`shift+tab` 与方向键切字段、`enter` 提交、`esc` 取消、逐字段校验错误内联展示。验证：`go test ./internal/tui -run Form`。
- [x] 2.2 表单在编辑期间 `InputMode` 返回 true，全局键（数字、`q`、`?`、`S`）不生效；取消时不留副作用。验证：`go test ./internal/tui -run Form`。
- [x] 2.3 长文本与自由属性字段走既有 `$EDITOR` 闭环（临时文件 600、退出后清理），不新增加密路径。验证：`go test ./internal/tui -run Form` 与人工编辑。

## 3. Tab 接入 [中优先级]

- [x] 3.1 Env Tab 接入：`r` 重命名 key、分组重命名、分组删除（带确认）；错误经统一提示条反馈。验证：`go test ./internal/tui -run Env`。
- [x] 3.2 Text Tab 接入：`r` 重命名 key、分组重命名/删除、从文件导入（路径字段 + `SetFromFile`）。验证：`go test ./internal/tui -run Text`。
- [x] 3.3 Config Tab 接入：重命名条目、编辑分组与描述（`SetMeta`），并在详情中展示元信息。验证：`go test ./internal/tui -run Config`。

## 4. 文档与整体验证

- [x] 4.1 更新 `README.md` 快捷键表与 `.agents/skills/senv-cli/SKILL.md` 的 TUI 键位说明（新增重命名、分组管理与导入）。验证：人工比对文档与界面按键。
- [x] 4.2 运行 `make check`，修复 fmt/vet/lint/race 问题。验证：`make check` 全部通过。
