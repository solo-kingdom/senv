## Context

text 已是独立 vault kind：`texts/{group}/{key}.enc`，AES-256-GCM，CLI/TUI/MCP/同步齐全。backup 要平行复制这条链路，但不复用 text 存储、分组或 Tab。产品决策见 proposal.md - Why，以及归档探索任务 `tasks/archive/2026-09-18/backup-feature/`。

git 模式对 vault 目录整体分发，新目录 `backups/` 会随 git 走，不经 syncschema 白名单。server 模式必须把新 kind 写入共享 `internal/syncschema`（client 收集/落地与 server 事务前校验同一包）。

## Goals / Non-Goals

**Goals:**

- 以 text 为模板落地独立 kind：存储、CLI、TUI Tab、MCP、同步
- 用三个子 change 切开文件范围，core 先行，surfaces 与 sync 不互相改同一批实现文件
- 存量 vault 不必重跑 `senv init` 也能写入 backup `default`

**Non-Goals:**

- 见 proposal.md - Non-goals
- 本 driver 不改代码；实现落在子 change

## Decisions

### D1 存储布局：`backups/{group}/{key}.enc`，元数据 `.meta.enc`

与 text 同构：分组目录 + 条目密文 + 组 meta。JSON 明文载荷含 `value`/`size`/`created_at`/`updated_at`/可选 `description`。目录 0700、文件 0600。备选「复用 `texts/` 加 kind 前缀」否决：会回到混用体验。

### D2 大小上限：独立常量 `MaxBackupSize = 512 * 1024`

只计 value 明文字节；超限拒绝。不复用 `MaxTextSize` 符号，避免一边改上限误伤另一边。数值与 text 相同。

### D3 引用：backup 完全置身 `{{…}}` 之外

`internal/ref` 的 type 仍只有 `env`/`text`。`senv backup get` 不提供 `-d/--decode`。`{{backup:…}}` MUST NOT 解析到 backup 条目。

### D4 同步 kind：`backup` / `backup_meta`，身份与 text 相同（grp+key / grp+空 key）

`collectEntriesDiff` 增 `backups/` 遍历；`entryLocation` 映射回原路径。冲突呈现对齐 text（可解密对比），不是 SSH 的元数据-only——backup 没有私钥本体红线，产品要求对齐 text。

### D5 切片：core → surfaces → sync

| 子 change | 范围 | 依赖 |
|-----------|------|------|
| `backup-feature-core` | storage/manager/CLI/init 与存量补建 default、backup-storage spec、ref/shorthand 隔离、skill 的 CLI 段 | 无 |
| `backup-feature-surfaces` | TUI Backup Tab、全局搜索、MCP 四件套、`senv_group_add kind=backup`、skill 的 TUI/MCP 段 | core |
| `backup-feature-sync` | syncschema、collect/land、冲突、server store 测试、skill 的同步段 | core |

surfaces 与 sync 实现文件不重叠，但都可能改 `.agents/skills/senv-cli/SKILL.md`：实施时串行改 skill，或按段落合并。同一工作树默认串行 apply。

### D6 存量 vault 的 `default` 组

`senv init` 预置 backup `default`。已有 vault：backup Manager 打开时幂等确保 `default` 存在（内置说明），避免老用户必须重跑 init。不隐式创建其它组。

### D7 根快捷不变

`senv <group:key>` 仍只写 text。backup 只走 `senv backup …`。

## 数据流

```
写入（CLI/TUI/MCP）
  → backup.Manager（组必须已存在，default 除外已由 D6 保证）
  → 校验 value ≤ MaxBackupSize、description ≤ 2048
  → AES-256-GCM 落盘 backups/{group}/{key}.enc

读取
  → 解密 JSON → stdout / 文件 0600 / MCP 正文（list 不含 value）

同步（server）
  backups/*.enc ── collect ── push ── syncschema.ValidateIdentity
       ▲                         │
       └── land ◀── pull ────────┘
git 模式：整目录 backups/ 随仓库走
```

CLI 示例：

```
senv backup group add notes --description "冷备份"
senv backup set notes:dump --file ./dump.txt
senv backup list notes
senv backup get notes:dump -o ./dump.txt
senv backup export notes:dump --path ./dump.txt
```

## 错误处理策略

- **fail-closed**：超 512KB、隐式建组、非法 group/key（含 `:`）、说明超 2048、导出路径含符号链接——拒绝且不写半份文件。
- **upsert**：`set`/`import` 覆盖已有 key，无确认，刷新 `updated_at`。
- **引用**：backup 内容即使含 `{{env:…}}` 也不解析；外界 `{{backup:…}}` 不解析。
- **同步**：新 client + 旧 server 携带 backup 条目的整批 push 会被未知 kind 拒绝（与 SSH 扩容相同）→ 发布须 server 镜像先行。

## 向后兼容

- 新目录、新 kind，不改 texts/envs 格式。
- 旧 client 看不见 `backups/`；git 模式下旧 client 可能把该目录当普通文件带上，但不解释。
- 回退：停用 CLI/TUI/MCP 后 `backups/` 成为孤儿密文；改回 text 分组约定需另做导入，无自动搬家。

## Migration Plan

1. 实现并测试三个子 change（core 先合并到任务分支）。
2. 发布 senv-server 镜像（白名单含 `backup`/`backup_meta`），再发 client。
3. 回滚 client 即停止新写入；server 上已存 backup 条目为零知识孤儿，再升级后可继续同步。

## Risks / Trade-offs

- [平行复制 text 造成三处漂移] → 子 change 以 text 测试为对照清单；skill/help 用 `go run . backup --help` 核验。
- [新 client + 旧 server 整批同步失败] → D4/Migration：server 先行。
- [TUI 多一个 Tab 拖慢启动] → 沿用现有「只加载当前 Tab」；backup 进快照与搜索标识，不进热路径引用。
- [SKILL.md 被 surfaces 与 sync 同时改] → 串行 apply 或分段合并，禁止并行写同一文件。

## Open Questions

无。产品已冻结；本设计只做实现映射与切片。
