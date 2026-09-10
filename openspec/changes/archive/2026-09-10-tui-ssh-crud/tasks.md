## 1. 存储层与 CLI [高优先级]

- [x] 1.1 `ssh.Manager` 新增 `RenameKeyPair(old, new)`：new 冲突拒绝，成功后原子改写引用它的 host `identityKey`；补单测覆盖冲突、无引用、多引用。验证：`go test ./internal/ssh`。
- [x] 1.2 `cmd/ssh.go` 新增 `senv keypair rename <old> <new>`，输出改名的 host 引用数量并记审计（`AuditOpSSHKey`）。验证：`go test ./cmd -run SSH` 与 `go run . keypair rename --help`。

## 2. TUI host 编辑 [高优先级]

- [x] 2.1 host 新建/编辑表单接入通用表单组件：alias、hostname、user、port、proxyJump（引用选择）、identityKey（keypair 选择，含「无」）、tags；`extra` 跳 `$EDITOR`；提交调用 `AddHost`/`UpdateHost`。验证：`go test ./internal/tui -run SSH`。
- [x] 2.2 校验复用既有规则：identityKey 与 proxyJump 引用不存在时表单内联报错且不写入。验证：`go test ./internal/tui -run SSH`。
- [x] 2.3 host 删除（确认）、导出 OpenSSH 片段（先预览再写目标文件或仅显示）。验证：`go test ./internal/tui -run SSH`。
- [x] 2.4 host 列表内联显示所用 keypair 与指纹摘要，长文本按截断规则处理。验证：`go test ./internal/tui -run SSH`。

## 3. TUI keypair 编辑与联动 [高优先级]

- [x] 3.1 keypair 导入：输入名称与私钥文件路径，复用 `ImportKeyPair`（默认拒绝重名覆盖）；表单错误内联展示。验证：`go test ./internal/tui -run SSH`。
- [x] 3.2 keypair 重命名：调用 `RenameKeyPair`，成功后刷新 host 引用显示。验证：`go test ./internal/tui -run SSH`。
- [x] 3.3 keypair 删除保护：被 host 引用时列出引用者并拒绝；提供显式输入的强制删除入口（清引用后删除）。验证：`go test ./internal/tui -run SSH`。
- [x] 3.4 materialize：确认框显示「将私钥明文写到 `~/.ssh/senv/<name>`（0600）」，目标已存在时二次确认；成功后提示落盘路径，不渲染私钥内容。验证：`go test ./internal/tui -run SSH` 与人工落盘检查。

## 4. 文档与整体验证

- [x] 4.1 更新 `README.md` 的 SSH 与 TUI 章节、`.agents/skills/senv-cli/SKILL.md`（keypair rename、TUI SSH 编辑与联动）。验证：人工比对文档与 `go run . keypair --help`。
- [x] 4.2 运行 `make check`，修复 fmt/vet/lint/race 问题。验证：`make check` 全部通过。
