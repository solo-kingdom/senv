## 1. 存储层校验函数（高优先级）

- [x] 1.1 在 `internal/storage/validate.go` 新增包级正则 `validEnvKeyRe = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")` 与函数 `ValidateEnvKey(name string) error`：不匹配时返回 `fmt.Errorf("%q is not a valid shell variable name: must match [A-Za-z_][A-Za-z0-9_]*", name)`。添加注释说明 POSIX shell 变量名规则及用途。
  - 验证：`go build ./internal/storage/...` 通过。
- [x] 1.2 在 `internal/storage/validate_test.go`（或新建）为 `ValidateEnvKey` 编写表驱动单测：合法用例（`API_KEY`、`_PRIVATE`、`a`、`A1_B2`）返回 nil；非法用例（空串、`123KEY`、`my-key`、`foo.bar`、`a/b`、`with space`、`colon:name`）返回非 nil error。
  - 验证：`go test ./internal/storage/ -run ValidateEnvKey -v` 全部通过。

## 2. env 写入校验集成（高优先级）

- [x] 2.1 在 `internal/env/manager.go` 的 `Set` 中，于既有 `storage.ValidateName(group)` / `storage.ValidateName(key)` 之后追加 `if err := storage.ValidateEnvKey(key); err != nil { return fmt.Errorf("invalid env key: %w", err) }`。仅作用于 key，不改 group 校验。
  - 验证：`go build ./internal/env/...` 通过；构造非法 key 调用 `Set` 返回 error 且文件未生成。
- [x] 2.2 在 `internal/env/manager_test.go`（或新建）为 `Manager.Set` 的 key 校验编写单测：合法 key 写入成功并可 `Get` 读回；非法 key（`a/b`、`my-key`、`1ABC`、空串）写入返回 error，且再次 `Get` 确认未落盘。
  - 验证：`go test ./internal/env/ -run TestSet -v`（含 race）通过。

## 3. export 历史非法 key 容错

- [x] 3.1 修改 `internal/env/manager.go` 的 `Export`：在拼装每行 `export` 前 `if err := storage.ValidateEnvKey(key); err != nil { fmt.Fprintf(os.Stderr, "warning: skipping invalid env key %q in group %q (rename or delete it)\n", key, group); continue }`。合法变量不受影响。注意引入 `os` 包。
  - 验证：手工构造一个含非法 key 的 group 文件后 `go run . env export`，stderr 出现 warning，stdout 不含非法 key 行。
- [x] 3.2 在 `internal/env/manager_test.go` 为 `Export` 容错编写单测：同一 group 内并存合法与非法 key 时，返回字符串仅含合法变量的 `export` 行；该场景下验证 stderr 警告（可注入 `io.Writer` 或通过子进程/输出捕获）。全部合法时无 warning。
  - 验证：`go test ./internal/env/ -run TestExport -v`（含 race）通过。

## 4. CLI / 快捷方式集成测试

- [x] 4.1 在 `cmd/address_key_integration_test.go`（或新建 `cmd/env_key_validation_test.go`）补充集成测试覆盖 spec 场景：`senv env set` 拒绝 `openviking/root_api_key`、`123KEY`、`my-key`、`foo.bar`；接受 `API_KEY`、`_PRIVATE`；快捷方式 `prod:my/key` 被拒、`prod:API_KEY` 被接受。
  - 验证：`go test ./cmd/ -run EnvKey -v`（含 race）通过。
- [x] 4.2 确认所有写入入口（CLI `env set`、`runEnvShorthand`、交互式 `setEnvVar`、TUI env tab）均经 `env.Manager.Set`，无需额外改动即可一致生效；如发现旁路则补校验。
  - 验证：`rg -n '\.Set\(' cmd/ internal/tui/` 确认 env 写入均收口于 `env.Manager.Set`。

## 5. 全量校验与收尾

- [x] 5.1 运行 `make check`（fmt + vet + lint + test，含 `-race`），修复全部告警与失败。
  - 验证：`make check` 退出码 0。
- [x] 5.2 手工冒烟（需已 init 的测试项目）：`senv env set API_KEY ok` 成功；`senv env set a/b fail` 报错；`eval $(senv env export)` 在 shell 中无 `not valid in this context` 报错。
  - 验证：shell 中 `echo $API_KEY` 输出 `ok`，且非法 key 场景给出清晰错误信息。
