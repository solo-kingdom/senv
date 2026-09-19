## 1. 存储与 Manager（高优先级·安全）

- [x] 1.1 `internal/storage`：`BackupDirName`、`MaxBackupSize`、条目/组 meta 读写（0700/0600、AES-256-GCM JSON：value/size/timestamps/description）、`ValidateName` 拒绝 `:`。验证：`go test ./internal/storage/ -race` 含 512KB 常量与超限拒绝
- [x] 1.2 `internal/backup` Manager：Set/Get/Delete/List/Import/Export/RenameKey/Group CRUD；Set 输入走文件/stdin/参数/编辑器优先级；禁止隐式建组；打开时幂等确保 `default`。验证：`go test ./internal/backup/ -race` 覆盖超限、缺组、upsert、default 补建

## 2. CLI

- [x] 2.1 `cmd/backup.go` 注册 `senv backup` set/get/list/delete/import/export 与 group list/add/delete；get 无 `-d`；group add `--description` 必填；delete 组要确认。验证：`go run . backup --help`、`go run . backup set --help`、`go run . backup group add --help`
- [x] 2.2 init 创建 backup `default`；根快捷 `senv <group:key>` 仍只写 text。验证：CLI 测试覆盖 init 后 `backup list`、根快捷写入 texts 而非 backups
- [x] 2.3 导出安全对齐 text（0600、拒符号链接、`--path` 必填、成功只打路径）。验证：`go test ./cmd/ -race` 相关用例

## 3. 隔离与说明

- [x] 3.1 `internal/ref`：type 仍仅 env/text；含 `{{backup:…}}` 的值不解到 backup。验证：ref 单测新增 backup 模板场景
- [x] 3.2 vault-description / group-threshold 行为落到 backup 条目与组（2048 字节、list 带说明不含 value）。验证：与 1.2/2.1 测试共用断言

## 4. 文档与收尾

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md` CLI/分组/init/引用隔离段。验证：对照 `go run . backup --help` 无过时命令
- [x] 4.2 `make check` 通过；backup-storage spec 场景与测试对照。验证：`make check`
