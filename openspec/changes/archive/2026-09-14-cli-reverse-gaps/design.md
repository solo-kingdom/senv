## Context

能力审计确认的两处 CLI 反向缺口（TUI 已有、CLI 无）。关键现状：

- TUI KeyPair Tab `e`：`mgr.UpdateKeyPair(name, fn(k){k.Group=g})`（internal/tui/keypair_tab.go:635），group 是唯一可就地编辑的 keypair 元数据；CLI `keypair` 无 edit（cmd/ssh.go:660-662）
- TUI Text Tab `i`：`mgr.SetFromFile(group,key,path)`，表单只校验 group/key 非空与命名合法、**不查重**——导入是 upsert（internal/tui/text_tab.go:982）
- TUI Text Tab `x` 单条导出：`mgr.GetToFile(group,key,path)` → `GetToFileWithMode(exportfile.DefaultMode=0600)` → `exportfile.WriteFile` 原子写（internal/tui/text_tab.go:1015）
- host 侧免编辑器范式：`hostEditCmd` 在 `cmd.Flags().Changed("group")` 时跳编辑器、单字段更新（cmd/ssh.go:304-317）
- `text set --file` / `text get -o` 功能上已覆盖导入导出，但无 import/export 动词，agent 与脚本没有稳定词汇可用

## Goals / Non-Goals

**Goals:**

- 三个新命令与 TUI 同语义：keypair 改组、text 文件导入（upsert）、text 明文导出（固定 0600）
- 全部复用既有 manager API 与 auditOp 辅助：零存储格式变更、零新依赖

**Non-Goals:**

- 不改 TUI 任何行为；不动 `text set`/`text get`/`keypair import` 现有语义
- MCP 不加工具（保持只读现状）
- `text export` 的 `--mode`（见 D4）

## Decisions

### D1 `keypair edit <name> --group <g>`：仅单字段、无编辑器

`--group` 显式变更（`cmd.Flags().Changed("group")`）→ `mgr.UpdateKeyPair(name, func(k *storage.KeyPairEntry) error { k.Group = group; return nil })`，与 TUI `e` 同一调用。未给 `--group` → 参数错误并提示用法（不静默启编辑器）。

- 理由：keypair 没有 host 那样的可编辑 profile（hostname/user/port 等字段不存在），key 材料 MUST NOT 进编辑器；group 是唯一可变元数据（internal/tui/keypair_tab.go:616 注释同述）。
- 备选：`keypair group <name> <g>` 新动词——与 `host edit --group` 范式分裂、help 发现性差；弃。
- 校验免费获得：`UpdateKeyPair` 内部 `validateGroup` 拒 `/` 与 CR/LF/NUL；keypair 不存在时 `loadKeyPair` 报错、零写入。
- 行为注意：改组只影响**未来** materialize 路径与导出 `IdentityFile` 路径推导；已落盘文件不迁移（与 TUI 一致，host export 会按需重落缺失密钥）。
- 审计：`auditOp(AuditOpSSHKey, "keypair:"+name, ok, "edit --group" / "edit 失败")`，与 CLI host edit 风格一致。

### D2 `text import <key|group:key> --file <path>`：TUI `i` 的动词化

`resolveAddressKey(args[0], textGroup)` 解析地址（地址组优先于 `-g`，与 set/get/delete 一致）→ `textManager.SetFromFile(group, key, path)`（内部 expandHome + os.ReadFile + Set）。

- upsert 语义：TUI `i` 不查重、`Set` 覆盖既有值并刷新 `updated_at`——CLI 保持一致，不加 `--force`/确认提示。
- `--file` 必填（MarkFlagRequired 或显式校验）：不回落 stdin/编辑器——这是与 `text set` 的明确分界，import 是无人值守动词。
- 审计：`auditOp(AuditOpText, "text:"+group+":"+key, ok, "import <path>"/"import 失败")`，与 TUI `recordAudit(..., "import "+path)` 同 detail。
- 子命令占用裸词：`senv text import`（无冒号）此前落到 Help；key 名恰为 `import`/`export` 时需写全 `g:import`。可接受——shorthand 本就以 `:` 为地址标记。

### D3 `text export <key|group:key> --path <path>`：TUI `x` 单条导出的动词化

`resolveAddressKey` 解析 → `textManager.GetToFile(group, key, path)`（固定 `exportfile.DefaultMode=0600`）。

- 不经引用解析：导出 vault 密文的逐字节解密值；解码结果导出仍归 `text get -o`。
- 成功仅打印路径，NEVER 打印值。
- 审计：不新增——导出是读取面；`text get -o` 与 TUI `x` 现状均无审计事件。
- `--path` 必填；不接受 stdout（要 stdout 用 `text get`）。

### D4 固定 0600、不提供 `--mode`

`text export` 固定 `exportfile.DefaultMode`，与 TUI `x` 完全对齐；显式放宽共享沿用 `text get -o --mode`（受 spec「Text 导出 mode 必须显式且有效」约束）。

- 备选：透传 `--mode`——与 get -o 重复、且需连改 mode requirement 扩大 spec 触面；弃。

## 数据流

```
用户命令                     cmd 层                              manager / internal 层
──────────────────────────────────────────────────────────────────────────────────────────
keypair edit k --group g  → keypairEditCmd (Changed("group"))   → ssh.Manager.UpdateKeyPair
                                                                  │ vault 读锁 → loadKeyPair
                                                                  │ → k.Group = g → validateGroup
                                                                  │ → saveKeyPair（密文回写）
text import g:k --file p  → textImportCmd (resolveAddressKey)    → text.Manager.SetFromFile
                                                                  │ expandHome → os.ReadFile(p)
                                                                  │ → Set（AES-256-GCM 加密写 vault）
text export g:k --path p  → textExportCmd (resolveAddressKey)    → text.Manager.GetToFile
                                                                  │ Get（解密）→ exportfile.WriteFile
                                                                  │   （原子写、父目录按需创建、0600）

审计：keypair edit / text import 成功与失败均 auditOp（AuditOpSSHKey / AuditOpText，detail 含
"edit --group" / "import <path>"）；text export 无审计事件（读取面，与 get -o / TUI x 现状一致）。
```

## 错误处理策略

| 场景 | 行为 |
|------|------|
| `keypair edit` 未给 `--group` | 参数错误 + 用法提示；零副作用、不启编辑器 |
| keypair 不存在 | `loadKeyPair` 错误原样返回；零变更 |
| `--group` 含 `/`、CR/LF、NUL | `validateGroup` 拒绝，vault 不变 |
| `text import --file` 缺失 | 参数错误，不读 stdin、不启编辑器 |
| `text import --file` 路径不存在 | `SetFromFile` 读文件错误；零存储变更，不产生半个条目 |
| `text import` 目标 key 已存在 | 覆盖（设计语义），`updated_at` 刷新；无确认提示 |
| `text export --path` 指向符号链接 | `exportfile.WriteFile` 拒绝；链接目标内容不变 |
| `text export` 目标已存在且权限宽松 | 原子写后收紧 0600 |
| `text export` key 不存在 | `Get` 解密读取错误原样返回；零文件副作用 |
| 任一命令成功 | 输出确认信息；export 只打印路径，不回显明文 |

## 使用示例

```bash
senv keypair edit web-key --group prod                 # 改组；--group "" 清除分组
senv text import notes:README --file ./README.md       # 新建或覆盖（upsert）
senv text export secrets:PRIVATE_KEY --path ~/keys/id.pem   # 0600 明文原子落盘
```

## Risks / Trade-offs

- `text import` upsert 无确认，脚本可能静默覆盖既有值 → 缓解：审计 detail 含源路径、`updated_at` 可追溯；spec scenario 明示覆盖语义。
- 裸词 `import`/`export` 被子命令占用 → 缓解：shorthand 以 `:` 为地址标记，`g:import` 写法不受影响；此前裸词本就落到 Help，无回归。
- 三个命令与 `set --file`/`get -o` 功能近交 → 接受：动词显式化是本 change 的目的（help/审计/技能文档可引用稳定词汇），实现层复用同一 manager 调用，无行为分叉。

## Migration Plan

无存储格式变更，新命令为纯增量，向后兼容；旧版本 client 行为不受影响。回滚 = 移除三个子命令注册，无数据迁移。
