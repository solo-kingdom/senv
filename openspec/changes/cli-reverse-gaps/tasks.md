## 1. keypair edit --group（高优先级：SSH 资产元数据写）

- [x] 1.1 cmd/ssh.go 实现 `keypairEditCmd`（`Use: edit <name>`）：`--group` 显式变更（`cmd.Flags().Changed`）时调 `mgr.UpdateKeyPair` 单字段更新，未给则参数错误提示用法；成功输出 `✓ Updated keypair <name>`；成功/失败均 `auditOp(AuditOpSSHKey, "keypair:"+name, ok, "edit --group" / "edit 失败")`；注册进 keypairCmd（验证：临时 vault 中 `edit k --group prod` 后 `keypair list` 展示新组；缺 `--group` 报错且零变更）
- [x] 1.2 1.1 配对测试（参照 cmd/ssh_test.go 的 `TestKeypairGroupFlagFlow` 临时 vault 模式）：改组生效、`--group ""` 清除分组、`--group a/b` 拒绝且原值不变、不存在 keypair 报错零副作用（验证：`go test ./cmd/ -race -run TestKeypairEdit` 全绿）

## 2. text import（高优先级：明文读盘入密库）

- [x] 2.1 cmd/text.go 实现 `textImportCmd`（`Use: import <key|group:key>`）：`--file` 必填（缺失即参数错误，不回落 stdin/编辑器）；`resolveAddressKey(args[0], textGroup)` 解析地址；调 `textManager.SetFromFile`；审计 detail `import <path>` / `import 失败`；注册进 textCmd（验证：临时 vault 中 import 新键后 `text get` 回读一致；`--file` 缺失与路径不存在均报错且 vault 无新条目）
- [x] 2.2 2.1 配对测试：新键创建、`g:k` 地址优先于 `-g`、已存在 key 覆盖且 `updated_at` 刷新、源文件内容不变、缺失文件零副作用（验证：`go test ./cmd/ -race -run TestTextImport` 全绿）

## 3. text export（高优先级：明文写盘）

- [x] 3.1 cmd/text.go 实现 `textExportCmd`（`Use: export <key|group:key>`）：`--path` 必填；调 `textManager.GetToFile`（固定 0600）；成功仅打印路径不打印值；不新增审计事件；注册进 textCmd（验证：临时 vault 导出后文件内容与导入值逐字节一致、权限 0600；`--path` 为符号链接时被拒绝且链接内容不变）
- [x] 3.2 3.1 配对测试：0600 权限断言、覆盖既有宽松文件后收紧、key 不存在零文件副作用、值含 `{{env:...}}` 引用时逐字节原样落盘（不经引用解析）、stdout 无明文泄漏（验证：`go test ./cmd/ -race -run TestTextExport` 全绿）

## 4. 文档与回归

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md`：新增 `keypair edit --group` 与 `text import`/`text export` 的用法与安全说明（AGENTS.md 要求用户可见命令同变更更新 skill）（验证：`go run . keypair edit --help`、`go run . text import --help`、`go run . text export --help` 输出与 SKILL.md 描述一致）
- [x] 4.2 全量回归（验证：`make check` 全绿；`go run . mcp list-tools` 输出无新增工具；`go run . text --help` 子命令列表含 import/export）
