## Why

命令 `senv text set -g {group} {key}` 是最常用的操作，但需要写出完整路径。提供 `group:key` 寻址语法，让高频写入操作减少击键次数。

## What Changes

- 新增根命令快捷方式：`senv group:key [value]` 等价于 `senv text set -g group key [value]`
- 新增 `text` 子命令快捷方式：`senv text group:key [value]` 触发 text set
- 新增 `env` 子命令快捷方式：`senv env group:key value` 触发 env set（value 必填）
- `address` 支持四种形式：`group:key`、`:key`（default group）、`group:`（key=`__default`）、`:`（default group + `__default` key）
- **BREAKING**：key 名和 group 名禁止包含 `:`，在 storage 层和 cmd 层均做校验
- `__default` 不作保留字，是普通 key 名，快捷方式和完整命令访问同一 entry

## Capabilities

### New Capabilities

- `group-key-shorthand`: `group:key` 地址语法作为 text/env set 命令的快捷方式，包含 address 解析逻辑和 `:` 字符的 key/group 校验规则

### Modified Capabilities

- `text-storage`: key 和 group 名增加禁止 `:` 的合法性校验（存储层）

## Impact

- `cmd/root.go`：新增 `RunE`，检测 `group:key` 模式
- `cmd/text.go`：新增 `textCmd.RunE`，处理快捷方式；`textCmd` 继承 `--file` flag
- `cmd/env.go`：新增 `envCmd.RunE`，处理快捷方式
- `internal/text/manager.go`：`Set` 方法增加 key/group 合法性校验
- `internal/env/manager.go`：`Set` 方法增加 key/group 合法性校验
- 无新依赖，无加密层变更
