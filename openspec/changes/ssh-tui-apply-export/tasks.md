## 1. 按键与作用域（design D1）

- [x] 1.1 sshTab 注册 `A` 键：host 栏 = 当前 host 所在组、侧栏 = 选中组（All = 全量），keyactions/help 两栏同步（`a` 已被多选占用，审计勘误；验证：临时 HOME 起 TUI 用例，两栏按 `A` 分别进入确认模式且标题含组名/「all hosts」）
- [x] 1.2 确认框实现：Render 预算组片段数/待落盘数/Include 状态/warning 计数，enter/y 执行、esc/n 取消（验证：用例断言确认框文本含计数；取消后临时 HOME 下无 groups/ 创建）

## 2. 执行与结果（design D3）

- [x] 2.1 异步 Cmd 调 `Manager.Apply` 并 toast 摘要（重建/落盘/跳过/注册/warning 计数），部分失败红色 toast + 首条错误（验证：用例执行后断言 toast 含 `applied`；构造 proxyJump 悬空断言按键直接报错 toast 且零文件副作用）
- [x] 2.2 1.1–2.1 配对单测覆盖三个作用域与取消路径（验证：`go test ./internal/tui/ -race -run Apply` 全绿）

## 3. 导出目标防护（design D4）

- [x] 3.1 批量目录与单条目标文件表单 validate 拒绝 `~/.ssh/senv` 内路径（含自身）并提示改用 `A`；两处 placeholder 改为中性用户目录（验证：表单用例分别提交 `~/.ssh/senv/groups` 与 `~/.ssh/senv/x.conf` 均内联报错、提交用户目录通过）
- [x] 3.2 3.1 配对单测（验证：`go test ./internal/tui/ -race` 全绿）

## 4. 文档与回归

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md` 的 SSH Tab 按键说明与 README TUI 一节（验证：所述按键与实现一致抽查）
- [x] 4.2 全量回归（验证：`make check` 全绿）
