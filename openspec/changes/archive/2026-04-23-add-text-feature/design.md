## Context

senv 已有 `env`（环境变量）和 `config`（配置文件）两个核心模块。env 存储短字符串，以 group 为单位加密到单个文件 (`env_{group}.json.enc`)。config 存储整个文件，也以单文件加密。

现在需要新增 `text` 模块存储长文本块（证书、密钥、模板等），并实现 env ↔ text 的交叉引用机制。

现有存储层 (`internal/storage/`) 提供 AES-256-GCM 加解密能力，env 和 config 共用同一套密钥派生体系。session 模块提供密码缓存，避免重复输入。

## Goals / Non-Goals

**Goals:**

- 提供安全的加密长文本存储，支持 group 分组
- text 每个条目独立文件存储，与 Git 版本管理友好
- 编辑器集成，支持多行/大文本编辑
- 实现跨 env/text 的引用系统，支持递归解引用和循环检测
- 对现有 env 模块最小化侵入

**Non-Goals:**

- 不做引用的实时校验（set 时不检查引用目标是否存在）
- 不做引用的反向追踪
- 不做 key 层级命名
- 不做文件名脱敏
- 不做 text group 的 activate/deactivate 机制
- 不引入新的外部依赖

## Decisions

### D1: text 存储 — 每个 key 一个文件

**选择**: `dataPath/texts/{group}/{key}.enc`

**替代方案**:
- 整 group 一个 JSON 文件（与 env 一致）：加密后整个 group 是一个 blob，Git 无法处理单 key 冲突
- 索引文件 + 散列文件名：增加复杂度，且 `ls` 可读性差

**理由**: 用户明确要求 Git 友好。每个 key 独立文件意味着：
- 改一个 key 只产生一个文件的 diff
- 合并冲突时影响范围最小
- `ls texts/{group}/` 即可查看所有 key

### D2: 加密文件内部格式

**选择**: 解密后为 JSON：

```json
{
  "value": "实际文本内容",
  "size": 2048,
  "created_at": "2026-04-20T10:30:00Z",
  "updated_at": "2026-04-22T15:20:00Z"
}
```

**理由**: `text list` 需要显示大小和更新时间，从加密 JSON 中读取即可，无需维护额外索引文件。text 数量通常有限，逐个解密读元信息的性能可接受。

### D3: 引用语法 — `:` 分隔，类型前缀必须

**选择**:
```
{{env:key}}           → 当前 group 优先 → default 兜底
{{env:group:key}}     → 显式指定 group
{{text:key}}          → 当前 group 优先 → default 兜底
{{text:group:key}}    → 显式指定 group
\{{...}}              → 转义，不解析
```

**替代方案**:
- `{{.KEY}}` 模板语法：与 Go template 冲突
- `{{env:group.key}}` 用 `.` 分隔：key 名不能含 `.`
- 无类型前缀自动推断：容易与原始文本中的 `{{...}}` 冲突

**理由**: 类型前缀（`env:`/`text:`）必须存在，避免误解析。`:` 分隔在 key 扁平命名（不含层级）的前提下不会冲突。

### D4: 解引用时机

**选择**:
- 存储时永远存原始模板，不做任何转换
- `env export` 自动解引用
- `env get`/`text get` 需 `-d/--decode` 才解引用
- 默认严格模式（引用目标不存在 → 报错），`--loose` 宽松模式（保留原样）

**理由**: export 的语义是"产出可用的 shell 值"，必须解引用。get 的默认行为是"看原始值"，与用户预期一致。

### D5: 引用解析模块位置

**选择**: 独立 `internal/ref/resolver.go`，env 和 text 通过 `ValueGetter` 接口解耦。

```go
type ValueGetter interface {
    GetEnvValue(group, key string) (string, error)
    GetTextValue(group, key string) (string, error)
}
```

**理由**: 引用解析是 env 和 text 的共享能力，不应放在任一模块内。接口设计使 resolver 不依赖具体实现。

### D6: 编辑器选择

**选择**: `$VISUAL` > `$EDITOR` > `nano` > `vim`

**替代方案**: 只用 `$EDITOR`（现有 config 模块的做法）

**理由**: `$VISUAL` 是 POSIX 标准中偏好全屏编辑器的环境变量，比 `$EDITOR` 优先级更高。加入 `nano` 作为兜底降低新用户门槛。

### D7: text set 输入优先级

**选择**: `--file` > stdin pipe > 命令行参数 > 编辑器

**理由**: 显式指定的输入源优先级最高。编辑器作为最终兜底，只在其他方式都不适用时触发。

## Risks / Trade-offs

**[性能] text list 需解密所有文件** → 每个 group 通常不超过几十个 key，实际影响可忽略。如未来成为瓶颈，可加索引文件优化。

**[安全] 编辑器临时文件** → 临时文件存 `/tmp/senv-text-XXXX.tmp`，权限 0600，用后 `defer os.Remove` 立即清理。如果进程被 kill，临时文件可能残留——但 `/tmp` 通常有定期清理。

**[引用] 循环引用** → 用 `visited` set 检测，设置最大递归深度 10 层。循环引用报错并显示引用链。

**[兼容] 现有 env 模块** → 只新增 `-d`/`--loose` flags，不修改现有行为。env export 自动解引用是行为变更，但之前值中不会有 `{{...}}`，所以无实际影响。

**[Git] 加密文件无法 diff** → 这是已有问题（env/config 同样存在）。text 的单文件策略至少将冲突范围缩到最小。未来可考虑增加 commit hook 辅助。

## 数据流图

```
text set 流程:
┌──────────┐    ┌───────────────┐    ┌─────────────┐    ┌──────────────┐
│ 输入源    │───▶│ 512KB 校验    │───▶│ JSON 封装   │───▶│ AES-GCM 加密 │───▶ 文件写入
│ file/    │    │ 超限报错      │    │ {value,size, │    │ storage.Save │    texts/{g}/{k}.enc
│ stdin/   │    └───────────────┘    │  created_at, │    └──────────────┘
│ args/    │                         │  updated_at} │
│ editor   │                         └─────────────┘
└──────────┘

text get -d 流程:
┌──────────────┐    ┌─────────────┐    ┌──────────────┐    ┌─────────────┐
│ 文件读取     │───▶│ AES-GCM 解密│───▶│ JSON 解析    │───▶│ 引用解析    │───▶ 输出
│ storage.Load │    │              │    │ 提取 value   │    │ ref.Resolve │
└──────────────┘    └─────────────┘    └─────────────┘    │ 递归+循环检测│
                                                          └─────────────┘
```

## 错误处理策略

| 场景 | 行为 |
|------|------|
| text 不存在 | `text <key> not found in group <group>` |
| group 不存在 | `group <name> does not exist` |
| 值超过 512KB | `text value exceeds 512KB limit (<actual> bytes)` |
| key 已存在 set | 覆盖（编辑器模式预填内容） |
| 引用目标不存在（严格） | `unresolved reference {{type:group:key}}: <reason>` |
| 引用目标不存在（宽松） | 保留 `{{...}}` 原样，stderr 警告 |
| 循环引用 | `circular reference detected: A → B → A` |
| 编辑器返回错误 | `failed to run editor: <error>` |
| stdin 为终端（非 pipe） | 不读取 stdin，走编辑器逻辑 |
