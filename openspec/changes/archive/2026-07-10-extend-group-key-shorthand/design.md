## Context

`group:key` 快捷语法（`parseAddress`）已实现于根命令与 `text`/`env` 父命令的 set 路径。子命令 `get`/`delete`/`set` 仍将含 `:` 的参数当作字面 key，但 `ValidateName` 禁止 key/group 含 `:`，导致此类调用必然失败。用户写入用 `senv feg:ACCOUNT val`，读取却需 `senv text -g feg get ACCOUNT`，心智负担高。

## Goals / Non-Goals

**Goals:**
- 在 6 个子命令中统一 address 解析
- 读写/delete 与 `-g key` 形式等价
- 复用 `parseAddress`，最小 diff

**Non-Goals:**
- 不改 config、list、group 管理命令
- 不改存储格式或引用系统

## Decisions

### 1. 新增 `resolveAddressKey(arg, flagGroup) (group, key)`

```
arg 含 ':'  → parseAddress(arg)，address 的 group 生效
arg 无 ':'  → (flagGroup, arg)，与现行为一致
```

**理由**：key/group 禁止 `:`，含冒号参数只能是 address，无歧义。  
**备选**：在 `-g` 与 address 冲突时报错 — 拒绝，增加无谓摩擦。

### 2. 接入点

| 命令 | 改动 |
|------|------|
| `text get/delete/set` | 首参经 `resolveAddressKey` |
| `env get/delete/set` | 同上 |

`text get` 的 `--copy`/`--output`/`--decode` 使用解析后的 group/key。`resolveValue` 的 `CurrentGroup` 同步使用解析 group。

### 3. 数据流

```
用户输入: senv text get feg:ACCOUNT
    │
    ▼
textGetCmd.RunE
    │
    ▼
resolveAddressKey("feg:ACCOUNT", textGroup="default")
    │  parseAddress → group=feg, key=ACCOUNT
    ▼
textManager.Get("feg", "ACCOUNT")
    │
    ▼
stdout / clipboard / file
```

### 4. 错误处理

- address 解析成功但 entry 不存在 → 保持现有 storage 错误信息
- `a:b:c` → 拆为 group=`a`, key=`b:c`；key 含 `:` 时 `ValidateName` 在 set 时报错，get/delete 报 not found（与现有一致）

### 5. CLI 示例

```bash
# 写入（已有）
senv feg:ACCOUNT "secret"

# 读取（新增等价）
senv text get feg:ACCOUNT
senv text get -g feg ACCOUNT          # 仍支持

# 删除
senv text delete feg:ACCOUNT

# env 同理
senv env get prod:DATABASE_URL
```

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| 用户习惯 `-g` 与 address 混用 | address 优先，文档说明 |
| `text set feg:` 打开编辑器 | 解析为 key=`__default`，与根命令一致 |
| 帮助文本未更新 | tasks 含 README/Use 字符串更新 |

## Migration Plan

纯 CLI 行为增强，向后兼容：无 `:` 的参数路径不变。无需数据迁移。发版说明补充示例如可。

## Open Questions

（无）
