## 1. 公共解析

- [x] 1.1 在 `cmd/shorthand.go` 新增 `resolveAddressKey(arg, flagGroup string) (group, key string)`，含 `:` 时调用 `parseAddress`，否则返回 `(flagGroup, arg)`；验证：单元测试覆盖 address 与纯 key 两种路径
- [x] 1.2 在 `cmd/shorthand_test.go` 补充 `TestResolveAddressKey`：`:key`、`group:key`、纯 key、address 优先于 flagGroup

## 2. text 子命令接入

- [x] 2.1 `text get`：首参经 `resolveAddressKey`，`--copy`/`--output`/`--decode` 使用解析后的 group/key；验证：`senv text get feg:ACCOUNT` 与 `senv text get -g feg ACCOUNT` 输出一致
- [x] 2.2 `text delete`：首参经 `resolveAddressKey`；验证：`senv text delete feg:ACCOUNT` 删除正确 entry
- [x] 2.3 `text set`：首参经 `resolveAddressKey`；验证：`senv text set feg:ACCOUNT val` 写入正确 entry

## 3. env 子命令接入

- [x] 3.1 `env get`：首参经 `resolveAddressKey`，`-d` 解引用使用解析 group；验证：`senv env get feg:KEY` 等价于 `-g feg`
- [x] 3.2 `env delete`：首参经 `resolveAddressKey`；验证删除正确 entry
- [x] 3.3 `env set`：首参经 `resolveAddressKey`；验证写入正确 entry

## 4. 边界与回归

- [x] 4.1 验证 address 优先于 `-g`：`senv text get -g other feg:ACCOUNT` 读取 `feg` 分组
- [x] 4.2 验证原有路径不变：`senv text get KEY`、`senv text set KEY val`、`senv env get KEY` 行为与改前一致
- [x] 4.3 验证根命令 / `text` / `env` 父命令 set 快捷方式未受影响
- [x] 4.4 运行 `make test` 全绿

## 5. 文档

- [x] 5.1 更新 `README.md`：补充 `text get feg:KEY`、`env get group:key` 示例
- [x] 5.2 更新相关子命令 `Use`/`Long` 说明，注明支持 `group:key` 地址
