## Context

b3b4c33 起 `LoadConfigIndex` 强制走 `normalizeConfigIndex`（internal/storage/boundaries.go），所有 map key 过 `securefs.ValidateSegment`，含 `:` 的旧名直接令加载失败；索引文件缺失时 `ErrNotExist` 也被原样抛出。既有规约 `config-grouped-storage` 要求破坏性操作遇坏索引 fail closed——本设计必须保留该性质，只放宽只读路径。

## Goals / Non-Goals

**Goals:**
- 空库（无索引文件）只读/创建可用
- 只读路径遇"仅不可移植"存量名隔离降级，警告可见
- 破坏性路径维持 fail closed，不引入数据丢失
- 提供显式 repair 命令一次性修复存量名

**Non-Goals:**
- 不放宽 create/rename 的新名校验
- 不做静默自动迁移
- 不改 securefs 通用段校验规则

## Decisions

### D1：身份错误分两类，先结构后可移植

校验顺序：先判结构性无效（`/`、`\`、`..`、NUL、绝对路径、卷标、key≠Name、EncryptedFile≠规范名）→ 整体 fail closed；结构通过后仅因 `:` 被拒 → 归入隔离集。实现上等价于：结构检查后，隔离集 = 仅含 `:` 的名字。替代方案"全部非法名都隔离"被否——路径穿越类错误在任何路径都不该被跳过。

### D2：新增带隔离信息的读取 API，旧 API 保持硬失败

新增 `LoadConfigIndexWithQuarantine() (index, quarantined, err)`；`LoadConfigIndex()` 委托之，隔离集非空即返回错误。收益：edit/export/install/delete/rekey/同步/create 等所有写路径**零改动**自动 fail closed（create 虽未列入既有规约，但写索引会丢弃隔离条目，同样必须拒绝）。只读调用方（List、Groups、CheckConsistency、TUI）切换新 API 并透出警告。

### D3：ErrNotExist → 空索引

`root.Read(ConfigIndexFile)` 返回 `errors.Is(err, os.ErrNotExist)` 时返回 `NewConfigIndex()`；其他错误照常传播。不改索引 JSON 格式。

### D4：repair 走确定性改写 + 受控的旧文件重命名

改写规则：`:` → `_`，结果必须过 `ValidateSegment` 且不与现有 key 或其他新名冲突，冲突即失败。交互模式允许用户对单条输入自定义新名；`--yes` 非交互仅接受建议名。索引侧（key/Name/EncryptedFile）在 vault 变更锁内一次写回；文件侧因旧名含 `:` 无法过 securefs 段校验，repair 使用私有的受控 `os.Rename`：词法校验新旧路径均位于 data 根内、目标无符号链接后执行，不开放为通用 API。

### D5：警告结构化透传

`List`/`Groups` 增加带警告返回值的变体（如 `ListWithWarnings` 返回 items + `[]QuarantineWarning{OldName, Hint}`）；CLI 以 `⚠` 打到 stderr 且退出码为 0，TUI 在警告/错误栏展示并附 `senv config repair` 指引。

## 数据流

```
config_index.json
      │ root.Read
      ▼
 ErrNotExist? ──是──▶ 返回空索引
      │否
      ▼
 结构校验（穿越/不一致/绝对路径）
      │失败                      │通过
      ▼                          ▼
 整体 fail closed        ValidateSegment 仅因 ":" 失败?
（所有调用方）              │是              │否
                           ▼                ▼
                    quarantined[]      正常条目
                           │                │
              ┌────────────┴────┐         │
              ▼                 ▼         ▼
        只读调用方          写路径调用方   只读/写路径
     （list/TUI/doctor：     （LoadConfigIndex
      跳过+警告，退出 0）     硬失败+修复指引）
```

repair 数据流：`LoadConfigIndexWithQuarantine` → 计算建议名 → 冲突/缺文件预检 → 用户确认 → 变更锁内（重命名 .enc + 原子写回索引）→ 复读校验隔离集为空。

## 错误处理策略

- 结构性无效：原样保留现有错误链，不降级
- 隔离：不作为 error 抛出，转换为 `QuarantineWarning`；只读命令退出码保持 0（数据本体健康，仅展示降级）
- repair 每一步失败（冲突、缺文件、rename 失败、写回失败）都在任何后续写入前停止；索引写回沿用 `AtomicWrite`，文件重命名失败则不写索引（顺序：先文件后索引，索引是唯一事实来源的描述者）
- 所有隔离/修复错误信息包含原条目名与 `senv config repair` 指引，不包含密文内容

## 向后兼容

- 索引 JSON schema 不变，只改值；修复后的索引旧版二进制仍可读（新名均为可移植单段）
- 回滚策略：直接换回旧二进制即可，repair 的改名对旧版是普通合法名

## 使用示例

```bash
# 查看会被怎么修（不落盘）
senv config repair --dry-run
#   feg:ai-ops-portal.pub -> feg_ai-ops-portal.pub

# 交互确认逐条修复
senv config repair

# 脚本/CI：接受建议名，跳过确认
senv config repair --yes

# 索引残留但 .enc 已丢失：显式丢弃这些陈旧条目
senv config repair --drop-missing --yes
```

## Risks / Trade-offs

- [repair 的受控 os.Rename 绕过 securefs 段校验] → 仅限 repair 私有 helper，词法围栏 + 无 symlink 检查 + 不导出；修复后的所有常规操作仍走 securefs
- [隔离集存在时 create 也被拒，用户可能困惑] → 错误信息明确"存在待修复配置，请先运行 senv config repair"
- [`:`→`_` 可能与现有名撞车] → 预检全部改写目标，任一冲突整体不执行，交互模式允许自定义名
- [同步场景下另一端仍是旧名] → repair 后正常 git/sync 推送即可，两端索引一致后隔离消失；不做自动跨端协调
