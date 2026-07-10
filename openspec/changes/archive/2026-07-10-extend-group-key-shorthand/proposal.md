## Why

`group:key` 快捷写入已支持（`senv feg:ACCOUNT value`），但 `text get feg:ACCOUNT` 会把整串当字面 key 查找，必然失败——因 key/group 禁止含 `:`。读写不对称造成高频误用。

## What Changes

- `text get/delete/set` 与 `env get/delete/set` 子命令支持 `group:key` 地址参数
- 新增 `resolveAddressKey(arg, flagGroup)` 公共解析，复用现有 `parseAddress` 规则
- 参数含 `:` 时 address 优先于 `-g` flag；无 `:` 时行为不变
- 更新 README 与 `group-key-shorthand` spec

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `group-key-shorthand`: 将 address 语法从仅 set 扩展到 get/delete/set 子命令

## Impact

- `cmd/shorthand.go`：新增 `resolveAddressKey`
- `cmd/text.go`、`cmd/env.go`：6 个子命令 RunE 接入解析
- `cmd/shorthand_test.go`：补充测试
- `openspec/specs/group-key-shorthand/spec.md`：需求增量
- `README.md`：示例更新

## Non-goals

- 不改动根命令 / `text` / `env` 父命令已有的 set 快捷方式
- 不扩展 `config`（无 group 概念）或 `list`（参数语义为 group 名）
- 不改变 `{{type:group:key}}` 引用语法

## 安全性分析

无加密或存储层变更；仅 CLI 参数解析，不引入新攻击面。
