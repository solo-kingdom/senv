## 1. 存储层校验

- [x] 1.1 在 `internal/text/manager.go` 的 `Set` 方法中添加 `validateName(key)` 和 `validateName(group)` 校验，拒绝含 `:` 的名称；验证：`senv text set -g "a:b" key` 和 `senv text set -g group "a:b"` 均报错
- [x] 1.2 在 `internal/env/manager.go` 的 `Set` 方法中添加同样的校验；验证：`senv env set -g "a:b" key val` 和 `senv env set -g group "a:b" val` 均报错
- [x] 1.3 提取公共校验函数至合适位置（`internal/storage` 包或各 manager 内），避免重复逻辑

## 2. address 解析工具函数

- [x] 2.1 在 `cmd/` 包中实现 `parseAddress(arg string) (group, key string, ok bool)` 函数：含 `:` 则解析，空 group → `"default"`，空 key → `"__default"`，无 `:` 则 `ok=false`；为该函数编写单元测试覆盖全部四种变体

## 3. cmd 层快捷方式

- [x] 3.1 在 `cmd/root.go` 中为 `rootCmd` 添加 `RunE`：`len(args)==0` 或首参数无 `:` 时调用 `cmd.Help()`；有 `:` 时解析 address 并执行 text set 逻辑（含 `--file` 支持）；在 `rootCmd` 上注册 `--file` flag 和 `-g`/`--group` flag（group 由 address 决定，flag 可忽略）
- [x] 3.2 在 `cmd/text.go` 中为 `textCmd` 添加 `RunE`，逻辑同上；将 `--file` flag 也注册到 `textCmd`（与 `textSetCmd` 共用同一变量）；验证 `senv text group:key`、`senv text :key`、`senv text group:`、`senv text :` 均工作正常
- [x] 3.3 在 `cmd/env.go` 中为 `envCmd` 添加 `RunE`：有 `:` 但无 value 时返回错误提示；有 `:` 且有 value 时执行 env set；验证 `senv env group:key value` 正常，`senv env group:key` 报错

## 4. 验证与回归测试

- [x] 4.1 验证原有子命令路由不受影响：`senv text set key`、`senv text get key`、`senv env set key val`、`senv text`（显示 help）、`senv env`（显示 help）均行为不变
- [x] 4.2 验证 `senv :` 打开编辑器编辑 `default:__default`；`senv mygroup: value` 写入 `mygroup:__default`；`senv text get -g mygroup __default` 可读取到相同 entry
- [x] 4.3 验证 `senv mygroup:mykey --file /tmp/test.txt` 从文件读取内容写入；`senv text mygroup:mykey --file /tmp/test.txt` 同样正常
- [x] 4.4 运行 `make check` 确保全部测试通过、无 lint 错误
