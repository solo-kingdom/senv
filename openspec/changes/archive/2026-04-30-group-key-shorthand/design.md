## Context

当前命令结构中，`text` 和 `env` 命令只有子命令，没有 `RunE`。要写入一个 text entry 最短需要：

```
senv text set -g mygroup mykey
```

Cobra 路由机制：当某个命令有子命令时，若第一个参数不匹配任何子命令，cobra 会：
- 若该命令定义了 `RunE` → 调用 `RunE`，将剩余参数传入
- 若没有 `RunE` → 报 "unknown command" 错误

## Goals / Non-Goals

**Goals:**
- 支持 `group:key` 地址语法作为 text/env set 的快捷方式
- 根命令、`text` 子命令、`env` 子命令均支持该语法
- `:` 字符在 key 名和 group 名中被禁止（storage 层 + cmd 层双重校验）
- `__default` 作为普通 key 名，`group:` 快捷方式访问同一 entry，不作保留字处理

**Non-Goals:**
- 不支持 get/delete 操作的快捷方式（只做 set）
- 不新增交互模式（env 无 value 直接报错）
- 不修改存储格式或加密方案

## Decisions

### 1. address 解析：`strings.SplitN(arg, ":", 2)`

用首个 `:` 分割，`SplitN(..., 2)` 确保只切一次。

| 输入 | group | key |
|------|-------|-----|
| `group:key` | `group` | `key` |
| `:key` | `default` | `key` |
| `group:` | `group` | `__default` |
| `:` | `default` | `__default` |

空 group → `"default"`，空 key → `"__default"`。
无 `:` 的参数不触发快捷逻辑，cobra 正常处理（子命令或报错）。

### 2. Cobra RunE 挂载位置

在三处各加 `RunE`：`rootCmd`、`textCmd`、`envCmd`。

```
senv group:key [value]       → rootCmd.RunE     → text set
senv text group:key [value]  → textCmd.RunE     → text set
senv env group:key value     → envCmd.RunE      → env set
```

`RunE` 中：
- `len(args) == 0` → 调用 `cmd.Help()`，不打开编辑器（保留原有 help 行为）
- `args[0]` 不含 `:` → 调用 `cmd.Help()` 或报 unknown command 错误
- `args[0]` 含 `:` → 解析 address，执行 set

### 3. `--file` flag 的继承

`--file` flag 目前只挂在 `textSetCmd`。快捷方式需要在 `textCmd` 和 `rootCmd` 上也注册同名 flag，在 `RunE` 中读取。

`envCmd` 不加 `--file`（env 值为单行，不需要文件导入）。

### 4. 校验双重防线

```
cmd 层（parseAddress 后立即校验）
  └─ 给出友好错误：key "foo:bar" 不能包含冒号

storage 层（text.Manager.Set / env.Manager.Set）
  └─ strings.Contains(key, ":") → error "key must not contain ':'"
  └─ group 同理
```

storage 层是最终防线，确保任何路径都无法写入含 `:` 的 key/group。

## Risks / Trade-offs

- **Breaking: 含 `:` 的旧 key 无法再写入** → 已有数据不受影响（只限制写入），读取路径不校验。若用户有含 `:` 的历史 key，需手动迁移。
- **`senv text` 无参行为不变** → 仍显示 help（RunE 中检查 len(args)==0），无 breaking change。
- **`__default` 无保留语义** → 用户可通过完整命令 `senv text set -g group __default value` 直接访问，与快捷方式访问同一 entry，行为一致。
