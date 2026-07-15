## Context

`senv env export` 输出 `export NAME=value` 语句交由 shell `eval`。当前 env key 仅通过 `storage.ValidateName` 校验（仅禁止含 `:`），导致含 `/`、空格、`-` 开头等字符的 key（如 `openviking/root_api_key`）能被写入并在 export 时引发 `(eval):export:1: not valid in this context` 报错。

所有 env 写入路径（CLI `env set`、根/`env` 的 `group:key` 快捷方式、交互式菜单、TUI）最终都收口于 `env.Manager.Set`，这是本设计的核心切入点。

**当前数据流：**

```
CLI / shorthand / interactive / TUI
            │
            ▼
   cmd 层 resolveAddressKey(arg)   ── 按 ':' 拆出 group/key（shorthand.go）
            │
            ▼
   env.Manager.Set(group, key, value)
            │
            ├── storage.ValidateName(group)   仅禁 ':'
            ├── storage.ValidateName(key)     仅禁 ':'
            ▼
   storage 加密落盘
            │
            ▼
   env.Manager.Export()  ── 拼接 "export <key>='<val>'" → shell eval 失败
```

## Goals / Non-Goals

**Goals:**
- 在写入时用 POSIX shell 变量名白名单（`^[A-Za-z_][A-Za-z0-9_]*$`）校验 env key，拒绝无法导出的 key。
- 单点校验于 `env.Manager.Set`，所有写入路径自动一致。
- 对历史已存的非法 key，export 时给出可定位的警告而非静默失败或中断。
- 保持 `group:key` 快捷方式语义不变。

**Non-Goals:**
- 不改写历史数据，不做自动迁移。
- 不收紧 text key / group name 的命名约束。
- 不引入转义或重命名映射。

## Decisions

### 1. 校验函数置于 storage 包，命名 `ValidateEnvKey`

在 `internal/storage/validate.go` 新增 `ValidateEnvKey(name string) error`，匹配 `^[A-Za-z_][A-Za-z0-9_]*$`（编译为包级 `var` 复用）。与既有 `ValidateName`（仅禁 `:`，服务于 group/path）并列，语义分层清晰。

**备选：** 直接在 `env.Manager.Set` 内联正则。被否——校验需在单测与 export 警告复用，独立函数更可测、可复用。

### 2. 校验时机：`env.Manager.Set` 入口

在 `Set` 既有 `ValidateName(group)` / `ValidateName(key)` 之后，追加 `ValidateEnvKey(key)`。先过 `:` 校验保证与快捷方式拆分逻辑一致，再做严格名校验。错误信息形如：`invalid env key %q: must match shell variable name (letters, digits, underscore; must not start with a digit)`。

**备选：** 在 cmd 层逐命令校验。被否——存在 4 个入口（CLI/shorthand/interactive/TUI），易遗漏且不一致；收口于 manager 最稳。

### 3. `group:key` 快捷方式兼容性

`parseAddress` 先按首个 `:` 拆分出 group 与 key，`:` 在此阶段已被消费。`Set` 收到的 key 不含分隔符，新校验作用于拆分后的 key，与快捷方式零冲突。`senv env prod:API_KEY val` 正常通过；`senv env prod:my/key val` 因 key 含 `/` 被拒。

### 4. Export 对历史非法 key 的容错

`Export` 拼装每行前调用 `ValidateEnvKey(key)`：合法 key 正常输出 `export` 行；非法 key 跳过该行并向 stderr 输出警告 `warning: skipping invalid env key %q in group %q (rename or delete it)`。合法变量不受影响，避免一颗老鼠屎坏整批 export。

**备选：** export 遇到非法 key 直接报错退出。被否——用户历史数据可能含多条，逐条定位更友好，且不应阻断其他合法变量导出。

### 5. 校验范围不含 group name

group name 作为存储目录/文件名组件，已由 `ValidateName`（禁 `:`）及文件系统约束保护；它不作为 shell 变量名导出，故无需套用 POSIX 名规则。

## Risks / Trade-offs

- **[向后兼容] 部分用户已有含 `-`/`.` 的 key** → 这些 key 之前 export 同样会失败，本变更只是把失败点前移并提示。提供 export 容错 + 明确错误信息，用户可自行 `env delete` 后用合法名重建。无存储格式变更，无需迁移方案。
- **[严格度] POSIX 白名单拒绝 `foo.bar`、`my-var` 等常见写法** → 这正是“无法作为环境变量导出”的定义。需要此类名字的场景请用 `text` 存储（text key 不受限）。
- **[误伤] `__default` 等下划线名** → 命中白名单（`_` 开头合法），不受影响。

## 错误处理策略

- **写入路径（Set）：** 非法 key 返回 error，cmd 层统一以 `❌/return err` 呈现，事务性失败（不落盘）。
- **导出路径（Export）：** 非法 key 不中断流程，单条跳过 + stderr 警告；其余合法变量继续导出。
- **校验失败信息** 始终包含违规 key 原值与命名规则摘要，便于用户修正。

## 使用示例

```bash
# 合法
senv env set API_KEY secret123
senv env set _PRIVATE value
senv env prod:DATABASE_URL "postgres://..."

# 快捷方式，合法 key
senv env prod:API_KEY secret

# 非法 → 被拒绝
senv env set openviking/root_api_key xxx
# ❌ invalid env key "openviking/root_api_key": must match shell variable name ...

senv env prod:my-key xxx
# ❌ invalid env key "my-key": ...

# export 遇到历史非法 key
senv env export
# warning: skipping invalid env key "openviking/root_api_key" in group "default" (rename or delete it)
# export API_KEY='...'
```

## Migration Plan

无数据/存储格式变更，无需迁移。部署即生效：新写入立即受校验；历史非法 key 在 export 时以警告提示，用户按需手动清理。回滚只需移除 `Set` 中的 `ValidateEnvKey` 调用即可恢复旧行为。

## Open Questions

无。命名规则采用 POSIX 标准定义，决策明确。
