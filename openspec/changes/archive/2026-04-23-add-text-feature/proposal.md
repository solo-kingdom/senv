## Why

senv 目前只支持通过 `env` 存储短文本环境变量，但用户经常需要安全存储长文本内容（SSH 密钥、证书、配置模板、脚本等）。这些内容不适合作为环境变量，直接放在命令行参数中体验差，且无法在 env 和 text 之间建立引用关系。需要新增 `text` 功能，支持加密存储长文本，并实现 env ↔ text 的交叉引用机制。

## What Changes

- 新增 `senv text` 命令组：支持 set/get/delete/list 子命令，用 `-g` 指定 group
- text 存储采用**每个 key 单独加密文件**（`dataPath/texts/{group}/{key}.enc`），便于 Git 冲突隔离
- 每个 text 值上限 512KB
- `set` 无参数时自动打开编辑器（优先级：`$VISUAL` > `$EDITOR` > `nano` > `vim`），已有 key 预填内容
- `set` 支持 `--file` 从文件读取、stdin 管道读取、直接传值
- `get` 支持 `-d/--decode` 解引用、`-o/--output` 写文件、`--copy` 剪贴板
- 新增**引用系统**：env 和 text 值中可嵌入 `{{env:group:key}}` / `{{text:group:key}}` 模板
- 引用语法：`{{type:key}}`（当前组优先→default 兜底）或 `{{type:group:key}}`（显式指定）
- 类型前缀必须存在（`env:` 或 `text:`），否则为原文本；`\{{` 转义
- 解引用时机：`env export` 自动解引用；其余命令需 `-d` 才解引用
- 默认严格模式（引用不存在则报错），`--loose` 保留未解析引用原样
- 递归解引用 + 循环引用检测
- `env get`/`env list` 新增 `-d/--decode` 和 `--loose` flags

## Capabilities

### New Capabilities

- `text-storage`: 长文本加密存储，支持 group 组织、编辑器编辑、多种输入输出方式
- `ref-system`: env ↔ text 交叉引用机制，模板语法、递归解引用、循环检测

### Modified Capabilities

（无已有 capability 需要修改，`env` 的 `-d/--decode` 是功能增强而非 spec 级别行为变更）

## Non-goals

- 不做引用的**实时校验**（set 时不检查引用是否存在，只在解引用时检查）
- 不做引用的**反向追踪**（不记录哪些值被引用了）
- 不做 key 层级命名（key 扁平，不支持 `/` 或 `.` 分层）
- 不对文件名做脱敏（key 名明文存储，与现有 env group 命名策略一致）
- 不做 text group 的 activate/deactivate（text 不参与 env export，纯分类用途）

## Impact

- **新增代码**: `internal/text/manager.go`、`internal/ref/resolver.go`、`cmd/text.go`
- **修改代码**: `internal/storage/manager.go`（新增 Text CRUD）、`internal/storage/types.go`（新增 TextEntry 类型）、`cmd/env.go`（新增 -d/--decode flags）、`internal/env/manager.go`（接入引用解析）
- **依赖**: 无新外部依赖，全部基于现有 cobra + crypto 体系
- **存储**: 新增 `dataPath/texts/` 目录结构，不影响现有 env/config 存储
- **安全性**: 引用解析不涉及新的加密操作，仅组合现有解密能力；编辑器临时文件用后立即删除
