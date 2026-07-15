## Why

`env export` 生成 `export NAME=value` 语句供 shell 执行。当 env key 含有 shell 变量名不允许的字符（如 `/`、空格、`-` 开头等，例：`openviking/root_api_key`）时，shell 在 `eval` 时报错 `(eval):export:1: not valid in this context`，导致整批 export 失败。当前这类非法 key 能被静默写入，直到 export 时才暴露，用户难以定位。应在写入时即拒绝，避免产生无法导出的脏数据。

## What Changes

- 新增 POSIX shell 变量名校验：env key 必须匹配 `^[A-Za-z_][A-Za-z0-9_]*$`，否则在 `Set` 时报错拒绝。
- 校验集中在 `env.Manager.Set`（所有写入路径的唯一收口：CLI `env set`、`group:key` 快捷方式、交互式菜单、TUI），无需逐处改造。
- `env export` 在遇到已存的非法 key 时输出明确警告（容错历史数据），不中断其余合法变量的导出。
- 校验仅作用于 **env key**；text key、group name 维持现有宽松规则（它们不作为 shell 变量导出）。
- **注意 `{group}:{key}` 特殊逻辑**：快捷方式先按 `:` 拆分出 group/key，再对 key 做名校验，二者不冲突——`:` 已在拆分阶段消费，key 本身仍走新校验。

## Non-goals

- 不改写或自动迁移历史已存的非法 key（仅通过 export 警告提示，由用户决定是否重命名/删除）。
- 不收紧 text key 与 group name 的命名约束（它们不是 shell 变量名）。
- 不引入新的 key 转义/映射机制（拒绝而非静默转换）。

## Capabilities

### New Capabilities
- `env-key-validation`: 定义 env 变量名的合法性规则与写入/导出时的校验与提示行为。

### Modified Capabilities
<!-- 无现有 capability 的 spec 级需求变更。group-key-shorthand 的拆分行为不变，新校验作用于拆分后的 key，属实现细节。 -->

## Impact

- 代码：`internal/env/manager.go`（`Set` 新增名校验、`Export` 新增警告）、`internal/storage/validate.go`（新增 `ValidateEnvKey` 工具函数）。
- 受益路径：CLI `env set`、根/`env` 父命令的 `group:key` 快捷方式、交互式 `setEnvVar`、TUI env tab 写入。
- 测试：`internal/env` 与 `internal/storage` 新增单测；`cmd` 新增针对 `senv env set` 与快捷方式拒绝非法 key 的集成测试。
- 安全性：校验为确定性的字符集白名单（正则），无引入额外攻击面；反而避免后续 export 时因非法变量名触发的 shell 解析异常。

## Security Analysis

本变更为正确性与可靠性增强。校验逻辑为纯函数式的白名单正则匹配（`^[A-Za-z_][A-Za-z0-9_]*$`），判定确定、无副作用、无 I/O，不存在注入或时序攻击面。其副作用是阻止写入无法被 shell 安全导出的 key，降低 `eval $(senv env export)` 场景下的解析异常风险。不触及加密、密钥派生与文件权限相关路径。
