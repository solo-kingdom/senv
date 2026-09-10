## 1. 全局导航 [高优先级]

- [x] 1.1 `model.go` 数字键改为动态映射已注册 Tab（`1`–`9`，越界忽略），保留 `Tab`/`Shift+Tab` 循环。验证：`go test ./internal/tui -run 'Tab|Number'`，并人工在 7 Tab 场景按 `6`/`7` 直达 History/Audit。
- [x] 1.2 补防回归测试：git 模式（3 Tab）下 `4`–`9` 不改变当前 Tab。验证：`go test ./internal/tui -run Number`。
- [x] 1.3 新增 `?` 键位总览 overlay：列出全局键与当前 Tab 键位，`?`/`esc` 关闭，`InputMode` 期间不触发。验证：`go test ./internal/tui -run Help`。

## 2. 搜索覆盖 [中优先级]

- [x] 2.1 `S` 全局搜索新增 SSH host（alias/hostname）与 LLM provider（alias/base_url 标识）数据源，结果保留类型徽标与跳转定位。验证：`go test ./internal/tui -run Search`。
- [x] 2.2 补安全测试：SSH 私钥内容、LLM 凭据引用之外的值不进入搜索结果。验证：`go test ./internal/tui -run Search`。

## 3. 统一反馈与截断 [高优先级]

- [x] 3.1 引入统一提示条（错误 > 警告 > 成功，成功提示超时自动消失），迁移 env/text 的 `flash` 与 AI 的 `notice`，删除 per-tab 字段。验证：`go test ./internal/tui -run 'Flash|Notice|Banner'`。
- [x] 3.2 落「面板内容不依赖 Width 换行」硬规则：列表与详情行统一走截断，详情改由 `enter` 弹层承载。验证：`go test ./internal/tui -run 'Truncate|Wrap|Pane'`，并人工检查 AI/SSH 长 `base_url`、长模型列表不再折行。
- [x] 3.3 统一空态与文案语言：所有 Tab 具备空态提示，新增文案用简体中文。验证：`go test ./internal/tui -run Empty`，人工过一遍各 Tab 空态。

## 4. 审计与同步可见性 [高优先级]

- [x] 4.1 TUI 写路径接入操作审计（复用 `session.AuditOp*`），覆盖 env/text/config 与后续 SSH/LLM 写操作，失败也留痕且不含值。验证：`go test ./internal/tui -run Audit` 与 `senv audit` 实查。
- [x] 4.2 Footer 常驻同步状态：显示待推送条数与上次同步时间，无自动同步能力时（git 模式/未开 auto_sync）隐藏。验证：`go test ./cmd -run TUI`（同步状态适配）与人工观察。
- [x] 4.3 写后异步 auto push（沿用 `autoSyncPushBudget`），退出前仍有 dirty 时在 TUI 内提示一次。验证：`go test ./internal/tui -run Sync` 与人工断网场景。
- [x] 4.4 Audit Tab 增加自由文本过滤（保留既有类型预设）。验证：`go test ./internal/tui -run Audit`。

## 5. 文档与整体验证

- [x] 5.1 更新 `README.md` 快捷键表与 `.agents/skills/senv-cli/SKILL.md` 的 TUI 键位说明。验证：人工比对文档与 `go run . tui --help`。
- [x] 5.2 运行 `make check`，修复 fmt/vet/lint/race 问题。验证：`make check` 全部通过。
