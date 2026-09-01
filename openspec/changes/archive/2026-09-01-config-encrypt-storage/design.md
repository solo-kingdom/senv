## Context

见 driver `config-encrypt-driver/design.md` D1/D2。现状：`storage.ConfigFile{Name, EncryptedFile, TargetPath, CreatedAt, UpdatedAt}`，`internal/config.Manager` 已有 Create/Edit/Export/Delete/List/Get，加密复用 `internal/crypto`（与 env/text 同 key）。`expandHome` 目前是 `internal/config` 里的私有函数，仅处理 `~`。

## Decisions

### D1: 加字段而非改结构
`ConfigFile` 增 `Group string json:"group,omitempty"` 与 `Description string json:"description,omitempty"`。`omitempty` 保证旧版本工具读新索引不受影响，新代码读旧索引零值即兼容。提供 `NormalizedGroup()` 或直接在使用处做 `if g == "" { g = "default" }`。

### D2: 展开函数收敛为 `ResolveTargetPath(raw string) (string, error)`
顺序：`~` 前缀展开 → `os.ExpandEnv` → 校验结果非空且不含残留 `$`（未定义变量视为错误）。放在 `internal/config`，替代现有 `expandHome`；Export 改为调用它。

### D3: Manager 接口增量
- `Create(name, sourcePath, targetPath, group, description string)`（group 为空 → default）
- `List(groupFilter string)`：空串列出全部，否则过滤
- `ConfigInfo` 增 `Group`/`Description`
- `SetMeta(name, group, description)` 用于后续更新 meta（可选，供 TUI/CLI 编辑）

## Risks / Trade-offs
- [残留 `$` 判定误伤字面值] → 仅当展开后仍含 `$` 时报错，错误信息指出原始写法，用户可用转义避免
- [旧索引不写回导致 group 信息不落盘] → 接受，仅在下次修改该配置时落盘

## Migration Plan
纯增量字段，旧数据读入即为 `default` 组；无迁移脚本，无回滚动作。
